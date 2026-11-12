---
title: 第 55 讲 · 日志：slog / zap 与结构化采样
description: 用 log/slog 写结构化日志，掌握级别、上下文与 trace_id 关联、采样与脱敏，并避免日志打太多
---

<StageHeader stage="L5 · 工程化与质量" level="★★★☆☆" time="40 分钟" goal="让线上问题可从日志还原" prereq="第 54 讲 · 基准测试、模糊测试与覆盖率" />

# 第 55 讲 · 日志：slog / zap 与结构化采样

> 线上出问题时，最怕的不是没有日志，而是日志一大堆却拼不出当时发生了什么：一行行自由文本的 `fmt.Println`，没有请求标识、没有关键字段、还把手机号也打了进去。日志的目标只有一个——**让一次线上事件能被还原、被检索、被聚合**。这一讲用 Go 1.21 起内置的 `log/slog` 把这件事做对，并重点讲清楚什么不该打。

## 这一讲你会得到什么
<CheckList id="goal-55" title="本讲目标" :items='["用 slog 输出带字段的结构化日志，而不是拼字符串","按级别与 trace_id 组织上下文，让同一请求可串联","通过采样、过滤与脱敏控制日志量并守住隐私"]' />

## 1. 先建立直觉

把日志想成快递面单：自由文本像在包裹上随手写一句「发错了」，谁也查不到；结构化日志则是印好的表格，订单号、时间、收件人、状态码各占一格，机器能按任意一格筛选。再给同一次请求的所有面单盖上同一个 `trace_id`，就能顺着编号把网关、业务、数据库三段记录串成一条完整轨迹。

<ConceptDiagram type="http-chain" title="一条日志如何串起整条请求" labels="请求进入::入口生成 trace_id|中间件::把 trace_id 放进 context|业务处理::带字段记录事件|日志后端::按 trace_id 检索聚合" caption="结构化字段加 trace_id，让线上问题可以被还原而不是靠猜" />

Go 1.21 把 `log/slog` 收进标准库，意味着结构化日志不必再先选第三方库。判断顺序应该是：先 `slog`，只有压测确认日志本身成为瓶颈时才考虑 `zap`，而且要通过替换 `slog.Handler` 来换实现，业务代码一行不动。

## 2. 语法与规则

第一块代码是最核心的形态：事件名稳定，字段成对追加，输出交给 Handler 决定格式。

```go
// 结构化日志：字段是键值对，可被检索与聚合
package main

import (
	"log/slog"
	"os"
)

func main() {
	// NewJSONHandler 每行输出一个 JSON 对象，便于日志系统解析
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	slog.Info("订单创建成功",
		"order_id", "A-1001",
		"amount", 280,
		"user_id", 7,
	)
	slog.Warn("库存偏低", "sku", "SKU-9", "left", 3)
}
```

```text
{"time":"2024-01-02T15:04:05Z","level":"INFO","msg":"订单创建成功","order_id":"A-1001","amount":280,"user_id":7}
{"time":"2024-01-02T15:04:05Z","level":"WARN","msg":"库存偏低","sku":"SKU-9","left":3}
```

第二块解决「串不起来」：`With` 派生一个预置字段的 logger，同时把 `trace_id` 放进 context，跨函数依旧带得上。

```go
// 用 context 传递 trace_id，让同一条请求的日志自动带上同一字段
package main

import (
	"context"
	"log/slog"
	"os"
)

type ctxKey string

const traceKey ctxKey = "trace_id"

// WithTrace 派生绑定 trace_id 的 logger，并把 id 放进 context
func WithTrace(ctx context.Context, base *slog.Logger, traceID string) (*slog.Logger, context.Context) {
	ctx = context.WithValue(ctx, traceKey, traceID)
	return base.With("trace_id", traceID), ctx
}

func main() {
	base := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	log, ctx := WithTrace(context.Background(), base, "t-8f3c")
	log.Info("请求进入", "path", "/order")
	handle(ctx, log)
}

func handle(ctx context.Context, log *slog.Logger) {
	// 从 context 取回 trace_id，跨函数仍能关联同一条请求
	tid, _ := ctx.Value(traceKey).(string)
	log.Info("查询完成", "trace_from_ctx", tid, "rows", 1)
}
```

```text
{"time":"...","level":"INFO","msg":"请求进入","trace_id":"t-8f3c","path":"/order"}
{"time":"...","level":"INFO","msg":"查询完成","trace_id":"t-8f3c","trace_from_ctx":"t-8f3c","rows":1}
```

第三块讲控制：用 `HandlerOptions` 把高频 Debug 挡在输出之外，敏感字段先脱敏再落盘。

```go
// 级别过滤与脱敏：Debug 默认不输出，敏感字段先打码再记录
package main

import (
	"log/slog"
	"os"
	"strings"
)

// mask 只保留手机号后四位，避免明文落盘
func mask(phone string) string {
	if len(phone) < 4 {
		return "****"
	}
	return "****" + phone[len(phone)-4:]
}

func main() {
	// LevelInfo 会过滤掉 Debug，直接减少高频日志量
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	log := slog.New(h)
	log.Debug("enter_func", "step", 1) // 不会输出
	log.Info("user_registered", "phone", mask("13800138000"))
	log.Info("pay_callback", "note", strings.ToUpper("ok"))
}
```

<OutputBox title="运行结果（时间字段为示意）" output="time=2024-01-02T15:04:05Z level=INFO msg=user_registered phone=****8000\ntime=2024-01-02T15:04:05Z level=INFO msg=pay_callback note=OK" note="Debug 一行被级别过滤掉，手机号只剩后四位。" />

<LineByLine title="逐行看一条结构化日志" code="logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))\nlogger.Info(order_created,\n    order_id, id,\n    amount, 280,\n)\nlogger.Warn(low_stock, sku, s)" :notes='["构造一个 JSON 输出的结构化 logger","事件名用稳定的短标识，别拼变量","字段成对出现：键、值","继续追加业务字段","调用闭合","WARN 单独记录可自愈的异常"]' />

### 结构化、级别、trace_id、采样为什么存在

每一条都能从「不用它会怎样」反推出来。

结构化日志针对的是自由文本不可检索：一行 `fmt.Println("订单 A-1001 创建")` 只能靠子串或正则匹配，值里含空格、换行，或者想按金额区间聚合时就失效；拆成固定事件名加独立键值字段后，日志系统能对 `order_id`、`status` 建索引、做聚合与告警。事件名必须稳定、不能拼变量，是因为 `订单 A-1001 创建` 与 `订单 A-1002 创建` 是两个不同的 msg，无法 group by；变量该进字段，不该进 message。

级别是「输出门槛」，而且门槛前移到了 Handler 入口：低于当前级别的事件在格式化之前就被丢弃，成本几乎为零；若先把字符串拼好再判断要不要打，格式化开销已经付掉了，高频 Debug 全开时这部分浪费随 QPS 线性增长。`trace_id` 必须作为独立字段、并放进 context，是因为拼进 message 只是一个词、无法按字段过滤；放进 context 则能跨函数、跨服务传递而不污染每个函数签名（呼应第 47 讲），由入口中间件统一注入，业务代码不必层层手传。

采样补的是级别管不到的那一维：级别只按「重要性」过滤，不按「频率」限流。同一类 INFO 事件每秒十万次时全量落盘照样打爆磁盘，按每 N 条留 1 条或按 trace_id 哈希采样，能保住统计代表性又控住量。脱敏必须在写入前完成，是因为日志会被复制到采集、归档、分析多个系统，一旦明文落盘，跨系统删除与追回几乎不可能，还附带合规风险——后端脱敏只能算第二道防线。

| 方案 | 可检索性 | 高频下的量 | 主要代价 |
| --- | --- | --- | --- |
| fmt.Println 自由文本 | 只能子串匹配 | 不可控 | 出事拼不出现场 |
| 结构化 + 全量 INFO | 可聚合 | 随 QPS 线性膨胀 | 磁盘与采集成本、检索变慢 |
| 结构化 + 级别 | 可聚合 | 挡住低价值事件 | 关键链路信息可能被一起省掉 |
| 结构化 + 级别 + 采样 + 脱敏 | 可聚合 | 可控 | 需要字段字典与采样策略 |

### 可验证实验：级别门槛到底省下多少

下面这段程序把同一个循环分别跑在 DEBUG 与 INFO 门槛下，用输出行数证明「丢弃发生在格式化之前」。

```go
package main

import (
	"bytes"
	"fmt"
	"log/slog"
)

func main() {
	for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo} {
		var buf bytes.Buffer
		h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})
		log := slog.New(h)
		for i := 0; i < 1000; i++ {
			log.Debug("高频事件", "i", i)
			log.Info("关键事件", "i", i)
		}
		lines := bytes.Count(buf.Bytes(), []byte{'\n'})
		fmt.Printf("门槛=%s 输出行数=%d\n", level, lines)
	}
}
```

```text
门槛=DEBUG 输出行数=2000
门槛=INFO 输出行数=1000
```

逐字段看：循环每次产生一条 Debug 与一条 Info，门槛为 DEBUG 时两类都输出，共 2000 行；门槛为 INFO 时 1000 条 Debug 在 Handler 入口就被丢弃，只剩 1000 行，且不产生任何格式化开销。把 1000 换成真实请求里的循环次数、每行按实际字节估算，就能算出一句话：「循环体里一条 Debug 日志，在目标 QPS 下一天会写多少 GB」——这正是下一节那起磁盘被打爆事故的算术来源。

## 3. 深入一步

级别不是装饰，它决定线上成本：

| 级别 | 用途 | 线上默认 |
| --- | --- | --- |
| DEBUG | 变量、分支等排查细节 | 关闭，按需临时开 |
| INFO | 关键业务事件 | 开启，但高频路径要采样 |
| WARN | 可自愈的异常与降级 | 开启 |
| ERROR | 需要人介入的失败 | 开启并接告警 |

级别之外，字段的粒度也要克制。把一整张订单对象塞进一个字段，等于放弃了可检索性；只留 `order_id`、`status`、`cost_ms` 几个能筛能聚合的键，排查时才有价值。错误日志额外带上 `error` 与错误码，才能和统一响应、监控告警对得上。

<Term word="结构化日志" text="把事件拆成固定事件名加若干键值字段的日志形态，机器可按字段检索、聚合与告警，而不是对整行文本做模糊匹配。" />

<Callout type="warn" title="易错点：日志打太多" text="在循环里逐条打 INFO、把整个请求体和响应体塞进 message、日志里带手机号与令牌，是三个最常见的反模式。后果是磁盘与采集成本暴涨、检索变慢、关键错误被淹没，还附带隐私合规风险。" />

### Enabled 短路省下了什么、又省不下什么：从调用约定到逃逸分析

「低于级别的事件在格式化之前就被丢弃」可以追到 `src/log/slog/logger.go`：`Logger.log` 的第一件事是 `l.Handler().Enabled(ctx, level)`，返回 false 就直接 return，连 `slog.Record` 都不构造。但这里有个极易漏掉的机制边界——**Go 的实参在进入函数体之前就已全部求值**。`slog.Debug("高频", "v", expensive())` 里的 `expensive()` 一定先执行完毕，结果装进 `...any` 变参，才轮到 `Enabled` 判断；级别过滤救不了参数里的副作用与昂贵计算。`slog.Record`（`src/log/slog/record.go`）本身用一个内联的 `front [5]Attr` 数组加备用的 `back []Attr`：字段数不超过 5 时零堆分配，超过才落切片；而 `...any` 装箱对字符串、大对象仍可能产生分配。

由此推出三条实践结论，每条都可证伪：把**原始值**作为字段传、别预先 `fmt.Sprintf`，否则级别过滤掉了日志、格式化的字符串分配已经白付；昂贵字段（序列化整个对象、查一次上下文）要么用 `if log.Enabled(ctx, slog.LevelDebug)` 手动守卫，要么实现 `slog.LogValuer` 惰性求值；字段数控制在 5 个以内更省，但别为省一次分配牺牲可检索性。上一节说「脱敏必须在写入前完成」，从机制看还要补一句：`mask(phone)` 这个调用本身在 Debug 被过滤时**依然会执行**，只是结果没有落盘——它省的是 I/O 与格式化，不是脱敏函数的计算。

可复现实验（Go 1.23.8 实测），用计数器与 `testing.AllocsPerRun` 把「省下/没省下」量化：

```go
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
)

var calls int

func expensive() string {
	calls++
	return "v"
}

func main() {
	h := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo})
	log := slog.New(h)
	ctx := context.Background()

	calls = 0
	log.Debug("高频", "v", expensive())
	fmt.Println("Debug 被过滤后 expensive 调用次数:", calls)

	calls = 0
	if log.Enabled(ctx, slog.LevelDebug) {
		log.Debug("高频", "v", expensive())
	}
	fmt.Println("加 Enabled 守卫后 expensive 调用次数:", calls)

	a := testing.AllocsPerRun(2000, func() {
		log.Debug("高频", "i", 1)
	})
	b := testing.AllocsPerRun(2000, func() {
		log.Debug("高频", "i", fmt.Sprintf("%d", 1))
	})
	c := testing.AllocsPerRun(2000, func() {
		if log.Enabled(ctx, slog.LevelDebug) {
			log.Debug("高频", "i", fmt.Sprintf("%d", 1))
		}
	})
	fmt.Printf("被过滤的结构化字段 allocs/op     = %.0f\n", a)
	fmt.Printf("被过滤但预先 Sprintf allocs/op  = %.0f\n", b)
	fmt.Printf("Enabled 守卫 + Sprintf allocs/op = %.0f\n", c)
}
```

```text
Debug 被过滤后 expensive 调用次数: 1
加 Enabled 守卫后 expensive 调用次数: 0
被过滤的结构化字段 allocs/op     = 0
被过滤但预先 Sprintf allocs/op  = 1
Enabled 守卫 + Sprintf allocs/op = 0
```

逐字段看：第一行 `1` 证明 INFO 门槛下 Debug 确实没输出，但 `expensive()` 仍被调用了一次——这是 Go 求值顺序，不是 slog 的疏忽；第二行 `0` 证明先问 `log.Enabled` 再决定是否构造实参，昂贵计算才真正被短路；第三组里结构化字段（常量 `1` 装箱 + 全程 `Enabled` 短路）是 `0 allocs/op`，预先 `fmt.Sprintf` 拼好字符串再传进去是 `1 allocs/op`（那条字符串在被丢弃前已经分配），加 `Enabled` 守卫后又回到 `0`。三行合起来给出可证伪的对照：级别门槛省的是 Handler 之后的格式化与写盘，省不了调用点之前的求值与装箱。

**边界与失效条件**：`Enabled` 只保证不构造 `Record`、不格式化、不写盘，不省实参求值，也不省 `...any` 对引用类型/大对象的装箱；`front [5]Attr` 是标准库实现细节，字段超过 5 个会触发切片分配；`slog.LogValuer` 自 Go 1.21 起才有。换成 `zap` 时零分配靠的是 `CheckedEntry` 池化而非 `front` 数组，`Enabled` 的语义边界仍在，但分配数字会不同——这也是「先 slog、压测确认瓶颈再换 Handler」的机制依据。

**版本前提**：`log/slog` 自 Go 1.21 内置，本实验在 Go 1.23.8（darwin/arm64）实测；1.21 到 1.23 之间 `HandlerOptions` 与默认行为有细节调整，`go build -gcflags=-m` 可看到 `... argument does not escape` 与 `func literal escapes` 一类诊断（呼应第 27 讲逃逸分析），具体结论随编译器版本变化。CI 用 `go-version-file: go.mod` 锁版本，才能保证这里 allocs 数字可复现。

### 一条日志穿过 Handler 的四道方法：WithAttrs / WithGroup / ReplaceAttr 的调用契约

前面把 `slog` 讲成「事件名加键值字段，格式交给 Handler」，上一节又追到 `Enabled` 短路与 `front [5]Attr` 的分配。但 `Logger.With`、`WithGroup`、`HandlerOptions.ReplaceAttr` 到底按什么顺序作用，还没从 `Handler` 接口的契约推出来——这直接决定了「脱敏该放在哪一层」「group 会不会把字段弄丢」。

`src/log/slog/handler.go` 的 `Handler` 接口只有四个方法，注释把每个的契约写死了：`Enabled` 在参数处理前调用；`Handle` 只在 `Enabled` 为真时调用，且必须遵守若干规则（零值 `Time`/`PC` 忽略、Attr 的值需 resolve、key 与 value 都是零值的 Attr 忽略、空 key 的 group 内联、无 Attr 的 group 忽略）；`WithAttrs` 返回一个「属性 = 原属性 + 参数」的新 Handler，且**Handler 拥有这个 slice**（可保留、修改或丢弃）；`WithGroup` 把名字追加到已有 groups，之后所有 Attr（无论来自 `With` 还是 `Record`）的 key 都要按 group 序列限定，名字为空时直接返回 receiver 本身。

关键机制在 `commonHandler`（约 195 行）：它用 `groups []string`、`nOpenGroups int`、`preformattedAttrs []Attr` 三个字段记录状态。`withAttrs`（225 行）会把新 Attr 追加进 `preformattedAttrs` 并记下当时已打开的 group 数；`withGroup`（257 行）只是 `h2.groups = append(h2.groups, name)`。而 `ReplaceAttr` 的契约更细（142–167 行）：它**不作用于 group 本身，只作用于 group 里的非 group 属性**，第一个参数 `groups` 是「当前打开的 group 序列」，注释明确「must not be retained or modified」；内置属性 `time / level / source / msg` 也会传给它（time 为零、或没开 `AddSource` 时省略）；源码还给了逐次调用的例子——`Int("a",1), Group("g", Int("b",2)), Int("c",3)` 会依次以 `nil,Int("a")`、`["g"],Int("b")`、`nil,Int("c")` 调用三次。

用一个自定义 probe Handler 把这条调用链打出来（Go 1.23.8，darwin/arm64）：

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
)

type probe struct {
	groups []string
	attrs  []slog.Attr
}

func (p *probe) Enabled(context.Context, slog.Level) bool { return true }
func (p *probe) Handle(_ context.Context, r slog.Record) error {
	var out []string
	r.Attrs(func(a slog.Attr) bool { out = append(out, a.Key+"="+a.Value.String()); return true })
	fmt.Printf("Handle: groups=%v 预置=%v 本次=%v\n", p.groups, keys(p.attrs), out)
	return nil
}
func (p *probe) WithAttrs(as []slog.Attr) slog.Handler {
	fmt.Printf("WithAttrs: 进来 groups=%v attrs=%v\n", p.groups, keys(as))
	q := *p
	q.attrs = append(append([]slog.Attr{}, p.attrs...), as...)
	return &q
}
func (p *probe) WithGroup(name string) slog.Handler {
	fmt.Printf("WithGroup: name=%s 之前 groups=%v\n", name, p.groups)
	q := *p
	q.groups = append(append([]string{}, p.groups...), name)
	return &q
}
func keys(as []slog.Attr) []string {
	var r []string
	for _, a := range as {
		r = append(r, a.Key+"="+a.Value.String())
	}
	return r
}

func main() {
	log := slog.New(&probe{})
	log = log.With("svc", "order").WithGroup("req").With("trace", "t-1")
	log.Info("created", "id", 7, "phone", "13800138000")
}
```

```text
WithAttrs: 进来 groups=[] attrs=[svc=order]
WithGroup: name=req 之前 groups=[]
WithAttrs: 进来 groups=[req] attrs=[trace=t-1]
Handle: groups=[req] 预置=[svc=order trace=t-1] 本次=[id=7 phone=13800138000]
```

逐字段：三次派生调用**严格按链式顺序**发生，每次都返回新 `probe`，原对象不变（这正是 `WithAttrs` 注释要求「复制而非共享 slice」的原因——本例用 `append([]slog.Attr{}, p.attrs...)` 显式复制，否则第二个 `With` 会污染第一个 logger 的 `attrs`）；`WithGroup("req")` 之后 `groups=[req]`，于是紧随的 `WithAttrs` 打印出 `groups=[req]`——它加的 `trace` 字段在语义上属于 `req` 这个 group；`Handle` 里把字段分成两拨：`预置`（`With` 累积的 `svc`、`trace`，存在 Handler 状态里）与 `本次`（`Info` 调用点传的 `id`、`phone`，存在 `Record` 里），两者分开存放、最后合并输出。这从可观察输出印证了接口注释：`Handle` 收到的 `Record` 只带本次字段，预置字段是 Handler 自己带着的。

由此推出 `ReplaceAttr` 用在哪：它作用在「合并后的每条非 group Attr」上，既能改内置的 `time/level/msg`，也能改 `With` 与 `Record` 里的字段，但**收不到 group 本身**。所以脱敏要按 `a.Key` 判断，不能指望 `groups` 参数里出现 `req` 就把整组丢掉——那个参数是「当前所在 group 路径」，用于给 `["g"]` 里的 `b` 做区分，不是 group 属性的名。可证伪的一条：`ReplaceAttr` 若返回零值 `Attr{}`，该字段被丢弃（源码 144 行），这比在写入前 `mask(phone)` 更彻底——后者仍输出一个 `phone=****` 键，前者连键都不出现；把 `WithGroup("")` 传空串，接口规定返回 receiver 本身，链式调用不会新增一层，`groups` 也不变。

**zap 的 `CheckedEntry` 池化**解释第二版留下的那句「换 zap 时零分配靠 `CheckedEntry` 池化」。读 `go.uber.org/zap@v1.24.0/zapcore/entry.go`：`_cePool = sync.Pool{New: ... return &CheckedEntry{cores: make([]Core, 4)}}`（36 行），`getCheckedEntry` 从池里取一个并 `ce.reset()`（清空 `Entry`、`ErrorOutput`、把 `cores` 逐个置 nil 再截成长度 0，220 行），`CheckedEntry.Write` 写完各 Core 后**把 `ce` 还回池**（`putCheckedEntry(ce)`，266 行）。它甚至用 `dirty bool`（215 行）做「best-effort detection of pool misuse」：若一个已被归还的 `ce` 又被 `Write`，`dirty` 为真就打一条 `Unsafe CheckedEntry re-use` 到 `ErrorOutput` 而不是静默写坏数据。机制上的取舍就清楚了：slog 用 `front [5]Attr` 栈上数组省小对象，zap 用 `sync.Pool` 复用整条 `CheckedEntry`（含预分配的 `cores` 切片），两者都为了「高频日志不产生每次分配」，但一个靠内联数组、一个靠对象池——这正是第二版「先 slog、压测确认瓶颈再换 Handler」在分配层的依据。可证伪：`sync.Pool` 里的对象会在 GC 时被清空（呼应第 46 讲），所以 `CheckedEntry` 的复用率随 GC 频率波动，压测里「零分配」不是恒定承诺；把 `ce` 在 `Write` 之后继续使用会触发 `dirty` 告警，说明它不是可以随便持有的普通值。

### 从 `io.Writer` 到 `write(2)`：`O_APPEND` 保证的原子性到底覆盖到哪

日志库的终点都是 `io.Writer`，而写文件最终落到 OS 的 `write(2)`。多个 goroutine（甚至多个进程）同时往一个日志文件追加时，「每条记录会不会互相覆盖」不是 Go 层能保证的，取决于打开文件时有没有 `O_APPEND`。把第二版「脱敏 / 级别 / 采样」之外的这一层补上，才解释得清「为什么生产上要么用日志库自己的锁、要么用追加模式」。

`os.OpenFile(path, flags, perm)` 的 `flags` 直接映射到内核的 open 标志。不带 `O_APPEND` 时，每个打开的文件描述各自持有独立的文件偏移量（offset），都从 0 开始；两个写者各写 200 行，第二个人写第 1 行时仍从 offset 0 起，把第一个人的内容覆盖掉。带 `O_APPEND` 时，内核保证「每次 `write(2)` 前把 offset 原子地移到文件末尾」，于是「定位 + 写」对同一文件的其他写者是不可分割的，每条记录追加在末尾而不覆盖。

实验（Go 1.23.8，darwin/arm64；8 个 goroutine 各写 200 条 60 字节定长记录，共 1600 条）：

```go
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
)

const G, LINES, PAYLOAD = 8, 200, 40

func scenario(path string, useAppend bool) (int, int) {
	os.Remove(path)
	flags := os.O_WRONLY | os.O_CREATE
	if useAppend {
		flags |= os.O_APPEND
	}
	var wg sync.WaitGroup
	for g := 0; g < G; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			f, err := os.OpenFile(path, flags, 0644)
			if err != nil {
				return
			}
			defer f.Close()
			for i := 0; i < LINES; i++ {
				line := fmt.Sprintf("g=%d i=%03d p=%s\n", g, i, strings.Repeat("x", PAYLOAD))
				_, _ = f.WriteString(line)
			}
		}(g)
	}
	wg.Wait()
	f, _ := os.Open(path)
	defer f.Close()
	total, complete := 0, 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		total++
		l := sc.Text()
		if strings.HasPrefix(l, "g=") && strings.HasSuffix(l, strings.Repeat("x", PAYLOAD)) {
			complete++
		}
	}
	return total, complete
}

func main() {
	t, c := scenario("/tmp/append_on.log", true)
	fmt.Printf("O_APPEND   : 行数=%d 完整=%d (预期=%d)\n", t, c, G*LINES)
	t2, c2 := scenario("/tmp/append_off.log", false)
	fmt.Printf("无 O_APPEND: 行数=%d 完整=%d (预期=%d)\n", t2, c2, G*LINES)
}
```

```text
O_APPEND   : 行数=1600 完整=1600 (预期=1600)
无 O_APPEND: 行数=200 完整=200 (预期=1600)
```

逐字段：带 `O_APPEND` 时 1600 条全部落盘、全部是完整记录（没有被截断或交错的半行）；不带时只剩 200 行——8 个写者都从 offset 0 起写、互相覆盖，最终文件里只剩「最后写满的那个 fd」的 200 行，且文件大小约 11800 字节（200 × 59），而 `O_APPEND` 那份是 94400 字节（1600 × 59），正好 8 倍。定长记录让「完整」判定可数：前缀 `g=` 且后缀是 40 个 `x` 才算一条，撕裂行会被排除。这个 8 倍差就是 `O_APPEND` 的可量化收益，也把「追加原子」从一句口号变成可代入的算式：写者数 `G`、每人 `LINES` 条、定长 `L` 字节，则 `O_APPEND` 文件 ≈ `G×LINES×L`，无它则 ≈ `LINES×L`。

**边界必须讲清，否则容易过度承诺**。`O_APPEND` 的原子性只覆盖「单次 `write(2)` 的定位 + 写入」这一步，不覆盖「一次 `WriteString` 被拆成多次系统调用」的情形：本例每条 60 字节远小于缓冲区，一次 `Write` 就是一次 `write(2)`，所以不撕裂；若单条记录大到超过内核单次写的上限（或写的是 pipe，POSIX 只保证 `PIPE_BUF` 以内原子），`O_APPEND` 仍保证**追加不覆盖**，但不保证**一条记录不被别的写者插在中间**——定位是原子的，写入本身可能分段。所以生产日志库通常自己做一层带锁的缓冲（`bufio.Writer` + mutex），把「一条日志 = 一次 write」在用户态先拼好；`O_APPEND` 解决的是多进程 / 多 fd 的覆盖，锁解决的是单条记录的完整性，两者管的不是同一件事。可证伪：把上面 `PAYLOAD` 调到几十 KB 再跑，`O_APPEND` 那份仍能得到 1600 行的总量，但会出现「完整」数小于总行数的交错行——原子追加 ≠ 原子记录。

这也回填了第二版「脱敏必须在写入前完成」的一层：日志一旦被 `write(2)` 追加进文件，就脱离了 Go 进程的控制，采集、轮转、归档都按字节复制；`ReplaceAttr` 返回零值丢弃字段、或 `mask` 打码，都是在这唯一一次 `write` 之前完成的。把「不泄露」押在日志后端，等于押在已经落盘的字节能被追回，机制上不成立。

**版本前提**：本机 Go 1.23.8（darwin/arm64）实测，`O_APPEND` 语义依 POSIX / Darwin 内核，Linux 上一致；`os.OpenFile` 的 flag 映射、`bufio.Scanner` 默认 64K 行上限是本机行为；zap 以 `go.uber.org/zap@v1.24.0` 为准（本机模块缓存内版本），`sync.Pool` 与 `dirty` 检测自该版本稳定；`log/slog` 自 Go 1.21 内置，`Handler` 接口契约与 `ReplaceAttr` 的逐次调用示例在 `handler.go` 注释中，1.21–1.23 间细节有调整但语义不变。第 46 讲展开 `sync.Pool` 的 GC 生命周期，本讲前面讲 `Enabled` 短路与 `front [5]Attr`，这两节补的是「日志从 Logger 到内核系统调用」中间被跳过的两段。

### 三条最常见的误解

**误解一：把级别调到 ERROR 最省心，反正错误都会打出来。** 反例：门槛设成 ERROR 后，请求进入、外部调用耗时、降级与重试、状态变更这些 INFO/WARN 全消失；真出事时日志里只剩一句「支付失败」，无法还原是哪个环节、哪次请求、耗时多少，等于把排查线索亲手删掉。级别是分层采样而不是一刀切：ERROR 接告警、INFO 记状态变化、DEBUG 按需临时开，关键链路信息不能靠调门槛省掉。

**误解二：结构化就是把 fmt.Sprintf 换成 JSON 输出。** 反例：写成 `log.Info(fmt.Sprintf("订单 %s 金额 %d", id, amount))`，输出虽是 JSON 却只有一个 msg 字段，订单号和金额都没法检索、没法按金额聚合，连「同事件统计次数」都做不到（msg 每条都不同）。正确做法就是本讲第一块代码：事件名固定，业务值以独立键值字段成对追加。

**误解三：脱敏交给日志后端或采集侧做就行。** 反例：手机号明文已经写进容器 stdout，并被采集、归档、备份到多个系统；事后要删得跨系统清理，审计上泄露也已既成事实。正确做法是 `mask` 在写入前完成（第三块代码），后端脱敏只是万一漏网时的第二道防线，不能当主防线。

## 4. 工程实践

真实场景里，日志量往往随 QPS 线性增长，因此「打什么」比「怎么格式化」更重要。建议只在状态变化处打：请求进入与结束、外部调用失败、降级与重试、金额与库存变更；循环内改打聚合结果，例如一批处理完只记 `count` 与 `errs`。高频 Debug 交给采样，例如每 100 条留 1 条，而不是全量落盘。

字段命名也要统一。同一个含义在不同服务里一会儿叫 `user_id`、一会儿叫 `uid`，聚合时就得写一堆兼容逻辑；团队应先定一份字段字典，把 `trace_id`、`order_id`、`cost_ms` 这类高频键固定下来，再由中间件统一注入，业务代码只追加自己的业务字段。

<Compare title="全量刷屏与克制记录" :panels='[{"label":"写法 A · 全量刷屏","code":"// 反例：循环里逐条打全量\nfor _, o := range orders {\n    logger.Info(order, o)\n}\n// 请求体与手机号明文落盘","note":"日志量随数据量线性膨胀，检索变慢且有隐私风险"},{"label":"写法 B · 聚合加脱敏","code":"// 正例：一批只记一条\nlogger.Info(batch_done, count, n, errs, k)\n// 高频 Debug 交给采样\n// 敏感字段先 mask 再记录","note":"量可控、可聚合，且不带敏感明文"}]' />

`zap` 的定位也该说清：它是高性能第三方实现，适合已确认日志是瓶颈的场景，但 API 与 `slog` 不通用。稳妥做法是在入口处统一用 `slog.Logger`，需要替换时只换 `Handler` 实现，业务代码不感知。另一个常见错误是把 `trace_id` 拼进 message 字符串里——那样它只是一个词，无法作为字段被检索；必须作为独立键值字段传入，或者由中间件统一注入 context。

<Callout type="tip" title="一条可执行的检查" text="在 code review 里搜 log.Info 与 log.Debug：出现在循环体内、或参数里出现 token、password、phone、id_card 的，一律要求改成聚合或脱敏后再合入。" />

### 一次真实故障：一条 Debug 日志打爆磁盘

现象：大促开始约二十分钟后服务陆续不可用，监控显示磁盘写满，但 QPS 与 CPU 都正常。排查路径：

1. `df -h` 确认数据盘 100%，`du -sh /var/log/*` 定位到该服务的日志目录占了绝大部分；
2. 抽样看日志内容，几乎全是同一条「进入循环处理 item」的 Debug；
3. 追配置，发现灰度环境为排查问题把 `LOG_LEVEL=debug` 带进了生产（配置漂移，呼应第 56 讲），而这段代码每个请求要循环上千次；
4. 算账：单请求 1000 行、每行约 200 字节即 200KB，按 1 万 QPS 估算每秒约 2GB，磁盘必然被打满；
5. 根因是三件事叠加——循环体内的高频日志、生产误开 DEBUG、既没有采样也没有输出限额。

修复分三层：循环内改打聚合结果（一批只记 `count` 与 `errs`）、生产级别固定 INFO 并禁止用环境变量随意开 DEBUG、给日志输出加采样与按大小的轮转。为什么这些规则要同时存在：级别挡「低价值事件」，采样挡「高频同类事件」，聚合减少「事件数量」，三者分别堵住不同的爆炸路径，缺任何一层，日志量都会随某个维度线性膨胀。替代做法与代价也清楚：只靠磁盘告警，发现时服务已经受影响；只靠采集侧限流，本地磁盘照样被打满；事后清理脚本不可持续。

配置可以被机器校验，不必靠发布清单上的人工勾选：

```bash
grep -E '^LOG_LEVEL=' deploy/prod.env
```

```text
LOG_LEVEL=info
```

逐字段看：`grep` 只输出匹配行，字段名与取值一目了然；CI 里可进一步用「匹配到 debug 就非零退出」让误配直接失败的断言，而不是打印出来等人看。把这类「配置即代码」的检查放进流水线，是第 56 讲配置校验与第 60 讲 CI 门禁的共同落点。

### 把日志约束接进 CI

```yaml
# .github/workflows/log-guard.yml（节选）
- uses: actions/setup-go@v5
  with:
    go-version-file: go.mod
- name: 生产日志级别
  run: grep -qx 'LOG_LEVEL=info' deploy/prod.env
- name: 禁止明文敏感字段
  run: |
    if grep -R -nE 'slog\..*"(token|password|phone|id_card)"' ./internal --include='*.go'; then
      echo "发现疑似明文敏感字段"
      exit 1
    fi
- name: 静态检查
  run: go vet ./...
```

第二条只是低成本兜底：它只能抓「slog 调用里直接出现敏感键名字面量」的情形，挡不住变量拼接，因此仍要配合脱敏函数与 code review，不能当成完备的隐私扫描。版本锁定同样重要：`slog` 自 Go 1.21 起内置，1.21 到 1.23 之间 Handler 选项与默认行为有细节调整，`go-version-file: go.mod` 保证 CI 与本地输出一致、结论可复现；`LOG_LEVEL` 作为受版本控制的配置被校验，正是「同一份镜像跑多套环境」的前提。

### 与 L10 实战工程结构的呼应

这套约定在 L10 是基础设施：`/projects/net-shortlink`、`/projects/micro-ecommerce` 都在入口中间件生成 `trace_id` 并注入 context，业务层只追加自己的字段；`/projects/data-log-etl` 本身就是日志处理管道，字段字典直接决定能按哪些维度聚合；`/projects/kv-3-async` 的 WAL 与恢复过程靠状态变更日志还原现场。再往后，第 57 讲统一响应里的错误码要写进 `error` 字段，第 59 讲 pprof 的耗时、第 80 讲链路追踪的 span，都靠同一个 `trace_id` 与日志关联。日志不是出事才想起来的补丁，而是从第一讲就要定好字段字典的地基。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l5-engineering/lesson-055-logging`](https://github.com/lfire/go/tree/main/examples/l5-engineering/lesson-055-logging)。clone 仓库后进入 `examples/l5-engineering`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="写一条可串联且脱敏的日志" task="用 slog 记录一次订单创建：带 trace_id 字段、金额字段，并把手机号脱敏后再记录。事件名保持稳定，不要拼进 message。" :hints='["用 logger.With(trace_id, tid) 派生带公共字段的 logger","手机号先经 mask 处理再作为字段值","其他业务信息都以键值对追加"]' solution="logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))\nlog := logger.With(trace_id, tid)\nlog.Info(order_created,\n    amount, 280,\n    phone, mask(phone),\n)" explain="事件名稳定、字段独立，trace_id 可被检索，敏感值先脱敏，这条日志才既能串联又不泄露隐私。" />

## 随堂检测

<Quiz no="55-1" q="关于 slog 的结构化字段，哪项做法是正确的？" :options='["把 trace_id 拼进 message 字符串里","把 trace_id 作为独立键值字段传入","所有信息都用 fmt.Sprintf 拼成一行","只在 ERROR 级别才记录字段"]' answer="1" explain="只有独立字段才能被日志系统检索与聚合；拼进 message 里的文本无法按字段过滤。" />

<Predict no="55-2" q="用 slog.LevelInfo 的 HandlerOptions 记录一条 slog.Debug，会输出吗？" :options='["会输出","不会输出","会自动升级成 WARN","会 panic"]' answer="1" output="（无输出）" explain="Handler 的 Level 是最低输出门槛，低于 INFO 的 Debug 在进入格式化前就被丢弃，成本几乎为零。" />

## 一页纸速记

<KnowledgeCard :points='["slog 是 Go 1.21 起的标准库，事件名稳定、字段成对追加","trace_id 作为独立字段并随 context 贯穿整条请求","级别控制输出门槛，高频路径用采样而非全量","敏感字段先脱敏，循环内改打聚合结果"]' :pitfalls='["日志打太多：循环里逐条打、请求体明文落盘","把 trace_id 拼进 message 导致无法检索","一上手就换 zap，业务代码被第三方 API 绑死"]' summary="日志的价值是还原现场：结构化字段加 trace_id，再用级别与采样把量控住。" />

## 小结与下一讲

- `log/slog` 用事件名加键值字段，机器可检索、可聚合；优先标准库。
- `With` 与 context 让 `trace_id` 贯穿全链路，级别过滤挡住高频 Debug。
- 反模式是日志打太多与隐私泄露：聚合、采样、脱敏三件事缺一不可。

[下一讲：配置：flag / env / viper / 配置中心](/engineering/config)
