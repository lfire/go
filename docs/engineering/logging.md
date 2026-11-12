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

## 4. 工程实践

真实场景里，日志量往往随 QPS 线性增长，因此「打什么」比「怎么格式化」更重要。建议只在状态变化处打：请求进入与结束、外部调用失败、降级与重试、金额与库存变更；循环内改打聚合结果，例如一批处理完只记 `count` 与 `errs`。高频 Debug 交给采样，例如每 100 条留 1 条，而不是全量落盘。

字段命名也要统一。同一个含义在不同服务里一会儿叫 `user_id`、一会儿叫 `uid`，聚合时就得写一堆兼容逻辑；团队应先定一份字段字典，把 `trace_id`、`order_id`、`cost_ms` 这类高频键固定下来，再由中间件统一注入，业务代码只追加自己的业务字段。

<Compare title="全量刷屏与克制记录" :panels='[{"label":"写法 A · 全量刷屏","code":"// 反例：循环里逐条打全量\nfor _, o := range orders {\n    logger.Info(order, o)\n}\n// 请求体与手机号明文落盘","note":"日志量随数据量线性膨胀，检索变慢且有隐私风险"},{"label":"写法 B · 聚合加脱敏","code":"// 正例：一批只记一条\nlogger.Info(batch_done, count, n, errs, k)\n// 高频 Debug 交给采样\n// 敏感字段先 mask 再记录","note":"量可控、可聚合，且不带敏感明文"}]' />

`zap` 的定位也该说清：它是高性能第三方实现，适合已确认日志是瓶颈的场景，但 API 与 `slog` 不通用。稳妥做法是在入口处统一用 `slog.Logger`，需要替换时只换 `Handler` 实现，业务代码不感知。另一个常见错误是把 `trace_id` 拼进 message 字符串里——那样它只是一个词，无法作为字段被检索；必须作为独立键值字段传入，或者由中间件统一注入 context。

<Callout type="tip" title="一条可执行的检查" text="在 code review 里搜 log.Info 与 log.Debug：出现在循环体内、或参数里出现 token、password、phone、id_card 的，一律要求改成聚合或脱敏后再合入。" />

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
