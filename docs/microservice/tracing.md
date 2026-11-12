---
title: 第 80 讲 · OpenTelemetry 链路追踪
description: 自建 Trace 与 Span、实现 W3C traceparent 注入提取，把一次请求跨服务完整串起来
---

<StageHeader stage="L7 · RPC 与微服务" level="★★★★☆" time="50 分钟" goal="把一次请求跨服务完整串起来" prereq="第 79 讲 熔断、降级、重试与雪崩防护" />

# 第 80 讲 · OpenTelemetry 链路追踪

> 一次下单请求会穿过网关、订单、库存、支付、数据库，任何一环变慢都会让用户感到卡。可日志分散在不同机器上，靠时间戳拼出全貌几乎不可能。链路追踪要做的，是给这次请求发一个全局唯一的「通行证」，让它经过的每一段都记下自己是谁、爹是谁、花了多久，最后拼成一张完整的调用树。本讲用标准库把 Trace、Span、context 传播、W3C traceparent 的注入与提取、采样和日志关联各写一遍，再对照 OpenTelemetry 的 API。理解注入与提取这两个动作，你就抓住了跨进程追踪的全部秘密。

## 这一讲你会得到什么
<CheckList id="l80-goals" title="本讲目标" :items='["用 TraceID、SpanID、ParentID 自建 Span 模型，理解调用树如何由 context 串起","实现 W3C traceparent 的 Inject 与 Extract，把上下文写进 HTTP 头再还原","区分头部采样与尾部采样，并用 TraceID 做确定性采样判定","在中间件里把 trace_id 注入日志，实现链路与日志互相关联"]' />

## 1. 先建立直觉

把一次请求想成一次跨城快递。整趟运输有一个运单号（TraceID），每经过一个中转站就生成一张交接单（Span），上面写着「上一站是谁」（ParentID）、「本站干了什么、花了多久」。只要运单号一路贴在包裹上，哪怕中转站分布在十个城市，最后也能按运单号把所有交接单拼回同一条完整路线。

难点在于「运单号怎么跨进程传下去」。进程内靠 `context` 携带当前 Span 就够了；一旦要发 HTTP 或 gRPC 请求，就得把上下文序列化成请求头——这就是 **Inject（注入）**；对端收到请求再从头里还原——这就是 **Extract（提取）**。W3C Trace Context 标准把这件事定成一个 `traceparent` 头，格式固定为「版本-TraceID-SpanID-标志位」。少了注入或提取中的任意一步，链路就会在那一跳断成两截。

<ConceptDiagram type="tracing" title="一次请求跨服务的 Trace 与 Span" caption="同一 TraceID 贯穿网关与下游，每个服务生成带 ParentID 的 Span，构成一棵调用树" />

<FlowTimeline :steps='[{"title":"入口建 Trace","desc":"网关生成 TraceID 与根 Span，决定采样","tone":"blue"},{"title":"Inject","desc":"把 traceparent 写进出站请求头","tone":"blue"},{"title":"Extract","desc":"下游从请求头还原上下文","tone":"green"},{"title":"创建子 Span","desc":"继承 TraceID 并记录 ParentID 与耗时","tone":"green"},{"title":"继续下传","desc":"调用更下游时再次 Inject","tone":"orange"},{"title":"上报聚合","desc":"Span 导出到后端，按 TraceID 拼成调用树","tone":"orange"}]' direction="horizontal" />

## 2. 语法与规则

### 2.1 Trace、Span 与 context 传播

Span 是追踪的最小单元：归属一个 Trace，记住父 Span，带开始时间与属性。`StartSpan` 从 ctx 里找父 Span，有就继承它的 TraceID，没有就开一条新 Trace——这样「同一条链路上所有 Span 共享 TraceID」是自然成立的。

```go
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type spanKey struct{}

// Span 是一次操作的最小追踪单元，归属某个 Trace，并记住父 Span。
type Span struct {
	TraceID    string
	SpanID     string
	ParentID   string
	Name       string
	Start      time.Time
	Attributes map[string]string
}

func newID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// StartSpan 从 ctx 取父 Span，有则继承 TraceID，无则开一条新 Trace。
func StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	parent, _ := ctx.Value(spanKey{}).(*Span)
	s := &Span{SpanID: newID(8), Name: name, Start: time.Now(), Attributes: map[string]string{}}
	if parent != nil {
		s.TraceID = parent.TraceID
		s.ParentID = parent.SpanID
	} else {
		s.TraceID = newID(16)
	}
	return context.WithValue(ctx, spanKey{}, s), s
}

func (s *Span) End() { /* 真实实现会上报耗时与属性 */ }

func main() {
	ctx, root := StartSpan(context.Background(), "gateway")
	_, db := StartSpan(ctx, "db.query")
	_, cache := StartSpan(ctx, "cache.get")
	fmt.Println("同一 TraceID:", root.TraceID == db.TraceID && db.TraceID == cache.TraceID)
	fmt.Println("db 父 Span:", db.ParentID == root.SpanID)
	fmt.Println("TraceID 长度:", len(root.TraceID), "SpanID 长度:", len(db.SpanID))
}
```

<OutputBox title="运行结果" output="同一 TraceID: true\ndb 父 Span: true\nTraceID 长度: 32 SpanID 长度: 16" note="16 字节 TraceID 与 8 字节 SpanID 转十六进制后分别是 32 与 16 位，正是 W3C 标准规定的长度。" />

### 2.2 W3C traceparent 的注入与提取

这是跨进程追踪最核心的两个函数。`Inject` 把 TraceID、当前 SpanID 与采样标志写成 `00-a-b-c` 格式的头；`Extract` 校验分段数与各段长度后还原。提取失败不该臆造 ID，而应返回 `ok=false` 让上层决定开新 Trace。

```go
package main

import (
	"fmt"
	"net/http"
	"strings"
)

const traceparent = "traceparent"

// Inject 把当前 Trace 上下文写进出站 HTTP 头，格式遵循 W3C Trace Context。
func Inject(h http.Header, traceID, spanID string, sampled bool) {
	flags := "00"
	if sampled {
		flags = "01"
	}
	h.Set(traceparent, fmt.Sprintf("00-%s-%s-%s", traceID, spanID, flags))
}

// Extract 从入站请求还原 Trace 上下文；缺头或格式不对就返回 ok=false。
func Extract(h http.Header) (traceID, spanID string, sampled, ok bool) {
	parts := strings.Split(h.Get(traceparent), "-")
	if len(parts) != 4 || parts[0] != "00" || len(parts[1]) != 32 || len(parts[2]) != 16 {
		return "", "", false, false
	}
	return parts[1], parts[2], parts[3] == "01", true
}

func main() {
	// 服务 A 出站：注入
	out := http.Header{}
	Inject(out, "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7", true)
	fmt.Println("注入头:", out.Get(traceparent))

	// 服务 B 入站：提取并继续同一条 Trace
	tid, sid, sampled, ok := Extract(out)
	fmt.Printf("提取: trace=%s parent=%s sampled=%v ok=%v\n", tid, sid, sampled, ok)

	// 完全没有头的请求，应当交给上层开新 Trace
	_, _, _, ok2 := Extract(http.Header{})
	fmt.Println("无头时 ok:", ok2)
}
```

一段典型的采样与导出配置，说明采样决策与传播格式通常由环境变量或配置文件给出：

```yaml
traces:
  sampler: parentbased_traceidratio
  ratio: 0.1            # 头部采样 10%
  exporter: otlp
  endpoint: collector:4317
propagators:
  - tracecontext       # W3C traceparent
  - baggage
```

<LineByLine code="parent, _ := ctx.Value(spanKey{}).(*Span)\nif parent != nil {\n    s.TraceID = parent.TraceID\n    s.ParentID = parent.SpanID\n}" :notes='["从 context 里取出父 Span，类型断言失败则得到 nil","存在父 Span 说明这是链路中间的一站，而不是入口","继承同一个 TraceID，保证整条链路标识一致","把父 SpanID 记为 ParentID，父子关系由此建立"]' title="Span 父子继承逐行拆解" />

## 3. 深入一步

### 3.1 采样、HTTP 集成与日志关联

全量采集在高 QPS 下成本极高，因此需要采样。头部采样在入口按 TraceID 做一次确定性判定，结果随 traceparent 的 `sampled` 标志一路继承，保证同一条链路要么全采、要么全不采。中间件则负责提取上下文，并把 trace_id 打进每条日志——这是链路与日志能互相跳转的关键。

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
)

// Sampler 按 TraceID 做确定性采样，保证同一条 Trace 各处结论一致。
type Sampler struct{ ratio float64 }

func (s Sampler) ShouldSample(traceID string) bool {
	var h uint32
	for _, b := range []byte(traceID) {
		h = h*31 + uint32(b)
	}
	return float64(h%100)/100 < s.ratio
}

func traceIDFrom(h http.Header) string {
	parts := strings.Split(h.Get("traceparent"), "-")
	if len(parts) >= 2 && len(parts[1]) == 32 {
		return parts[1]
	}
	return "4bf92f3577b34da6a3ce929d0e0e4736" // 无头时新生成，这里用固定值演示
}

// TracingMiddleware 提取上下文，并把 trace_id 注入日志，实现日志与链路关联。
func TracingMiddleware(sample Sampler, next http.Handler) http.Handler {
	logger := log.New(os.Stdout, "", 0)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid := traceIDFrom(r.Header)
		if sample.ShouldSample(tid) {
			logger.Printf("trace_id=%s path=%s event=start", tid, r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	ts := httptest.NewServer(TracingMiddleware(Sampler{ratio: 1},
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintln(w, "ok")
		})))
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/order", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	resp, _ := http.DefaultClient.Do(req)
	fmt.Println("响应状态:", resp.StatusCode)
	resp.Body.Close()
}
```

| 概念 | 含义 | 是否跨进程 |
| --- | --- | --- |
| Trace | 一次请求贯穿所有服务的完整链路 | 靠 TraceID |
| Span | 链路里的一次操作，带父子关系与耗时 | 靠 SpanID 与 ParentID |
| Context | 进程内携带当前 Span 的载体 | 否，仅进程内 |
| 传播 Propagation | 把上下文写进 header 再还原 | 是，靠 Inject 与 Extract |

| 采样策略 | 决策时机 | 优点 | 代价 |
| --- | --- | --- | --- |
| 头部采样 | 入口一次决定，随 Trace 继承 | 省资源、链路完整 | 偶发错误请求可能被丢 |
| 尾部采样 | 收集完整条链路后再决定 | 能保留异常与慢链路 | 需缓冲全量，成本高 |
| 固定比例 | 按 TraceID 哈希 | 简单、分布均匀 | 低流量时样本偏少 |

<Term word="TraceID" text="一次请求全链路唯一的 16 字节标识，是把所有 Span 拼回一棵树的钥匙" />
<Term word="Span" text="链路中一次操作的记录，含 SpanID、ParentID、开始时间与属性" />
<Term word="traceparent" text="W3C 定义的传播头，格式为版本、TraceID、SpanID、标志位四段" />

<Callout type="warn" title="链路最容易断在异步与跨协议处" text="起 goroutine 或丢进消息队列时若不显式把带 Span 的 ctx 传下去，子任务会开一条全新的 Trace。gRPC 走 metadata、HTTP 走 header，两边必须用同一套 propagator，否则跨协议调用就串不起来。baggage 里别放用户手机号等敏感信息，它会随每一跳传播。" />

### 3.2 W3C traceparent 逐字段与确定性采样

跨进程追踪的全部秘密，就压缩在 `traceparent` 这一个头里。它的格式是 `版本-TraceID-SpanID-标志位` 四段，每段长度与含义都由 W3C Trace Context 标准定死：

| 段 | 长度 | 含义 | 为什么这样定 |
| --- | --- | --- | --- |
| version | 2 位十六进制 | 版本，当前为 00 | 留给未来格式演进，解析时先判版本 |
| trace-id | 32 位十六进制（16 字节） | 全链路唯一标识 | 128 位足够全球唯一，跨厂商不冲突 |
| parent-id | 16 位十六进制（8 字节） | 当前这一跳的 SpanID | 单条链路内唯一即可，故比 TraceID 短 |
| trace-flags | 2 位十六进制 | 最低位 01 表示 sampled | 把采样决定随链路继承下去 |

用一段标准库代码就能把各段长度与采样位量出来：

```go
package main

import (
	"fmt"
	"strings"
)

func main() {
	h := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	parts := strings.Split(h, "-")
	fmt.Printf("版本=%s TraceID长度=%d SpanID长度=%d 采样=%s\n",
		parts[0], len(parts[1]), len(parts[2]), parts[3])
}
```

<OutputBox title="运行结果" output="版本=00 TraceID长度=32 SpanID长度=16 采样=01" note="四段长度与标准完全一致，末段 01 表示这条链路被采样。" />

逐字段解释：`版本=00` 是当前 W3C 版本；`TraceID长度=32` 对应 16 字节，`SpanID长度=16` 对应 8 字节；`采样=01` 的最低位为 1，表示入口决定采集这条链路。 Extract 严格校验分段数与各段长度，格式不对就返回 `ok=false` 而不臆造 ID，正是因为这些长度是硬契约。

采样为什么必须「确定性」？如果每个服务各自按随机数决定采不采，同一条链路会有的 Span 被采、有的被丢，拼出来的调用树全是缺口。头部采样在入口按 TraceID 做一次确定性判定，再把结果写进 `sampled` 标志随 traceparent 一路继承；下游用 parentbased 策略尊重上游的决定，于是这条链路要么全采、要么全不采。尾部采样反过来，先缓冲完整条链路再决定，能保留异常与慢链路，代价是成本高、要缓冲全量。

### 3.3 传播推导与替代方案取舍

进程内靠 `context` 携带当前 Span 就够了；一旦要发 HTTP、gRPC 或消息，就得把上下文序列化进请求——这就是 Inject，对端再还原——这就是 Extract。两者缺一，链路就断在那一跳。W3C 还定义了 `baggage`，用来带灰度标、租户这类业务标签，但它会随每一跳传播，绝不能放手机号等敏感信息。

难点在「不同协议用不同载体」：HTTP 走 header、gRPC 走 metadata（map[string][]string）、消息队列走消息属性。全链路必须统一用同一套 propagator，否则一次 HTTP 转 gRPC 的调用就会因为载体和键名不一致而串不起来。

| 采样策略 | 决策时机 | 优点 | 失败边界 |
| --- | --- | --- | --- |
| 头部采样 | 入口一次决定，随 Trace 继承 | 省资源、链路完整 | 偶发错误请求可能被丢 |
| 尾部采样 | 收完整条链路再决定 | 能保留异常与慢链路 | 需缓冲全量，成本最高 |
| 全量采集 | 不采样 | 排查最完整 | 高 QPS 下存储与网络不可承受 |
| 固定比例 | 按 TraceID 哈希 | 简单、分布均匀 | 低流量时样本偏少 |

| 追踪体系 | 标准化 | 侵入性 | 失败边界 |
| --- | --- | --- | --- |
| OpenTelemetry | 事实标准，厂商中立 | SDK 插桩 | 依赖采样与导出配置 |
| Jaeger / Zipkin | 较成熟 | 需适配各自 propagator | 跨体系传播格式不一 |
| 自研 | 完全可控 | 每语言自实现 | 与生态工具不兼容 |
| eBPF 无侵入 | 无需改代码 | 零侵入 | 拿不到应用级语义属性 |

| 上下文放哪 | 用途 | 风险 |
| --- | --- | --- |
| traceparent | 只放 TraceID、SpanID、采样位 | 字段极简，几乎无风险 |
| baggage | 灰度标、租户等业务标签 | 随每跳传播，别放敏感信息 |
| 自定义 header | 内部扩展 | 跨协议易漏、易与标准冲突 |

### 3.4 可验证实验：断链复现与跨协议透传

「起 goroutine 不传 ctx 会开一条新 Trace」可以用一段同步代码清晰地看到，不必依赖真实网络：

```go
package main

import (
	"context"
	"fmt"
)

type spanKey struct{}

func traceID(ctx context.Context) string {
	if v, ok := ctx.Value(spanKey{}).(string); ok {
		return v
	}
	return "new-trace"
}

func main() {
	parent := context.WithValue(context.Background(), spanKey{}, "t-9527")
	fmt.Println("传 ctx:", traceID(parent))
	fmt.Println("断链:", traceID(context.Background()))
}
```

<OutputBox title="运行结果" output="传 ctx: t-9527\n断链: new-trace" note="同一份逻辑，传了父 ctx 就并入原链路，用 Background 起新 ctx 就另开一条 Trace。" />

逐字段解释：`传 ctx: t-9527` 说明子任务从 `parent` 里读到了原来的 TraceID；`断链: new-trace` 说明用 `context.Background()` 新建的 ctx 里没有任何 Span，只能另起一条链路。把 `parent` 显式传给 goroutine 或消息发送函数，是异步与跨协议场景不断链的唯一办法。

跨协议透传则用命令验证：

```bash
# HTTP：把 traceparent 作为 header 发出
curl -s -H 'traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01' \
  http://localhost:8080/order

# gRPC：同样的键放进 metadata
grpcurl -plaintext -rpc-header \
  'traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01' \
  -d '{"name":"go"}' localhost:50051 demo.v1.Greeter/SayHello
```

逐字段解释：两条命令用的是**完全相同的 traceparent 值**，只是载体从 HTTP header 换成 gRPC metadata；服务端只要用同一套 propagator 提取，就能把两次调用挂到同一条 Trace 上。若网关转发时漏掉这个头，下游 Extract 返回 `ok=false`，链路就在这一跳断成两截——这正是生产里最隐蔽的 trace 断链。

### 3.5 一次请求跨三跳的传播时序

把 Inject 与 Extract 放进真实调用链，能看清同一 TraceID 如何贯穿多个服务、采样决定如何继承。每一跳都做两件事：入站 Extract 还原上游上下文并创建子 Span，出站再 Inject 给下一跳；漏掉任意一次，链路就在那一跳断成两截。

<SequenceDiagram title="同一 TraceID 跨三跳的 Inject 与 Extract" caption="入口采样一次，逐跳提取再注入，所有 Span 共享 TraceID 并按 ParentID 串成调用树" :actors='["Client","Gateway","Order","Stock"]' :steps='[{"from":"Client","to":"Gateway","label":"入口建 Trace","note":"生成 TraceID","kind":"call"},{"from":"Gateway","to":"Gateway","label":"头部采样","note":"决定 sampled","kind":"self"},{"from":"Gateway","to":"Order","label":"Inject 写头","note":"traceparent 出站","kind":"call"},{"from":"Order","to":"Order","label":"Extract 还原","note":"创建子 Span","kind":"self"},{"from":"Order","to":"Stock","label":"再次 Inject","note":"继续下传","kind":"call"},{"from":"Stock","to":"Stock","label":"创建子 Span","note":"继承 TraceID","kind":"self"},{"from":"Stock","to":"Order","label":"返回结果","note":"带 status","kind":"return"},{"from":"Order","to":"Gateway","label":"返回结果","note":"沿链回传","kind":"return"},{"from":"Gateway","to":"Client","label":"返回结果","note":"闭环","kind":"return"},{"from":"Stock","to":"Stock","label":"End 上报","note":"导出 Span","kind":"self"}]' />

## 4. 工程实践

真实 OpenTelemetry 里，这些能力落在几个标准件上：`TracerProvider` 负责创建 Tracer 与配置采样、导出器；`otel.GetTextMapPropagator().Inject(ctx, carrier)` 与 `.Extract(ctx, carrier)` 对应本讲的 Inject/Extract，只是 carrier 泛化成了 HTTP header 或 gRPC metadata；`otelhttp` 与 `otelgrpc` 提供自动插桩，把进出站传播都包好了。理解标准库版本后，换 API 只是换个名字。

<Compare :panels='[{"label":"各打各的日志","code":"log.Printf(order failed)\nlog.Printf(payment failed)","note":"两个服务的日志没有共同 key，排查只能靠时间戳硬凑，跨服务问题几乎查不动"},{"label":"统一带 trace_id","code":"log.Printf(trace_id=%s failed, tid)","note":"一条 TraceID 把网关、订单、支付全串起来，日志与 Span 可互相跳转"}]' title="日志能不能串起一次请求" />

落地时还有几条经验。采样率按流量与成本调，错误与慢请求可以做尾部补采，避免「出事的那条恰好没采到」。Span 命名用低基数、语义化的名字（如 `GET /order` 或 `db.query`），别把用户 ID、订单号塞进 Span 名，否则时间序列会爆炸；这类高基数字段放 attributes。每个 Span 至少打上 `rpc.method`、`http.status_code`、`error` 三类属性，排查才有抓手。日志里统一输出 trace_id 与 span_id，配合第 55 讲的结构化日志，就能从一条日志直接跳到整条调用树。最后记得在 exit span 里 `End()` 并 `defer`，漏了上报会造成链路缺口，看起来就像某个服务凭空消失。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l7-microservice/lesson-080-tracing`](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-080-tracing)。clone 仓库后进入 `examples/l7-microservice`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

### 4.2 链路追踪的失败矩阵

追踪的坑大多「不报错、只缺数据」，排查时才发现链路是碎的：

| 故障 | 触发点 | 现象 | 防线 |
| --- | --- | --- | --- |
| trace 断链 | 起 goroutine 或发消息不传 ctx | 子任务开新 Trace，链路被切成两段 | 显式传 ctx + 统一 propagator |
| 网关漏透传 | 转发未带 traceparent | 链路缺网关这一跳 | 中间件统一 Inject 与 Extract |
| 采样丢错误链路 | 头部固定比例采样 | 出事的那条恰好没被采到 | 错误与慢请求做尾部补采 |
| Span 名基数爆炸 | 把 user_id 塞进 Span 名 | 时间序列爆炸、后端被打垮 | 高基数字段放 attributes |
| baggage 泄露 | 放手机号等敏感信息 | 随每一跳扩散 | 只放低敏标签 |
| 跨协议串不起来 | HTTP 与 gRPC 用不同 propagator | 调用树在协议边界断裂 | 全链路统一 W3C propagator |
| 时钟偏差 | 多机时间不同步 | Span 耗时错乱、父子区间重叠 | NTP 同步 + 只信相对耗时 |

### 4.3 两条真实工程误解

**误解一：起 goroutine 会自动继承 trace 的 ctx，不用显式传。** `go` 关键字只复制栈，不会把当前函数的 `context.Context` 自动塞进新协程；若协程里用 `context.Background()` 起新 ctx，父 Span 就丢了，子任务会开一条全新的 Trace。消息投递同理，ctx 必须作为消息属性或显式参数带过去。

<Compare :panels='[{"label":"错误：goroutine 用 Background","code":"go func() { work(context.Background()) }()","note":"新建 ctx 丢掉父 Span，子任务开一条全新 Trace，链路断成两截"},{"label":"正确：显式传 ctx","code":"go func(ctx context.Context) { work(ctx) }(parent)","note":"把带 Span 的 ctx 传下去，子任务并入同一条 Trace"}]' title="异步任务怎么不断链" />

**误解二：采样率越高越好，Span 名里放 user_id 方便排查。** 全量采集在高 QPS 下存储与网络成本不可承受；而把用户 ID、订单号塞进 Span 名更危险——每个取值都会生成一条独立时间序列，直接把追踪后端打垮。Span 名必须低基数、语义化（如 `GET /order` 或 `db.query`），高基数字段放 attributes，配合 [/engineering/logging](/engineering/logging) 的结构化日志按 trace_id 关联即可定位到具体用户。

<Callout type="warn" title="Span 名别放高基数字段" text="Span 名要用低基数的语义化名字，把 user_id、order_id 塞进名字会让每个取值生成一条独立时间序列，把追踪后端打垮；这类字段应放 attributes，靠 trace_id 关联日志去查。" />

### 4.4 与后续阶段的呼应

trace_id 是贯穿可观测三支柱的那根线：[/engineering/logging](/engineering/logging) 的结构化日志靠它把分散日志串起来，[/distributed/sre](/distributed/sre) 的告警降噪与故障定位依赖完整调用树，[/distributed/high-availability](/distributed/high-availability) 的多活排障要看跨机房链路，[/distributed/kubernetes](/distributed/kubernetes) 里请求跨多个 Pod 时更需要统一传播。到 L10，[/projects/micro-seckill](/projects/micro-seckill) 排查一次超时要靠全链路 Span 找到慢在哪一跳，[/projects/micro-ecommerce](/projects/micro-ecommerce) 的订单链路则把网关、订单、库存、支付用同一 TraceID 拼成一棵树——本讲 Inject 与 Extract 这两个动作，就是这棵树能拼出来的全部前提。

<Quiz q="W3C traceparent 头里，采样决定是靠哪一段随链路继承下去的？" :options='["version 段","trace-id 段","parent-id 段","trace-flags 的最低位"]' answer="3" explain="trace-flags 最低位为 1 表示 sampled，头部采样在入口决定一次，下游用 parentbased 尊重这个标志，保证同一条链路要么全采要么全不采。" />

<Predict q="按 3.4 的代码，把带 Span 的 parent ctx 与 context.Background() 分别传给 traceID，两行输出是？" :options='["两行都是 t-9527","传 ctx 为 t-9527，断链为 new-trace","两行都是 new-trace","第一行直接 panic"]' answer="1" output="传 ctx: t-9527；断链: new-trace" explain="传了父 ctx 就读到原 TraceID 并入同一链路；用 Background 新建的 ctx 里没有任何 Span，只能另开一条 Trace，这正是异步断链的根因。" />

## 动手练一练
<Exercise title="给 gRPC metadata 补上 traceparent 的透传" task="参照 2.2 的 Inject/Extract，把它们改成基于 map[string][]string 的 carrier，模拟 gRPC metadata；再写一个客户端拦截器，在每次调用前注入、服务端拦截器提取并创建子 Span。" :hints='["gRPC metadata 是 map[string][]string，key 用小写 traceparent","Inject 把格式化字符串放进切片第一个元素，Extract 从切片首元素读","客户端拦截器与服务端拦截器分别调用 Inject 与 Extract，共用一个 propagator"]' solution="要点一：定义 Carrier map[string][]string，Inject 用 Set(key, value)，Extract 用 Get(key) 取首元素。\n要点二：客户端拦截器从 ctx 取当前 Span，Inject 到 outgoing metadata，再发起调用。\n要点三：服务端拦截器 Extract incoming metadata，得到 TraceID 与 ParentID 后 StartSpan 创建子 Span，并把新 ctx 传给 handler。" explain="HTTP 与 gRPC 只是 carrier 不同，Inject 与 Extract 的语义完全一致；把传播收进拦截器，业务代码就完全不用感知。" />

## 随堂检测
<Quiz no="80-1" q="W3C traceparent 头里，把一次请求跨进程串起来的关键字段是？" :options='["只有 span-id","32 位十六进制的 trace-id，以及用作父子关系的当前 span-id","HTTP 请求方法名","采样率配置字符串"]' answer="1" explain="TraceID 是全链路唯一标识，所有服务继承它；当前 SpanID 则成为下一跳 Span 的 ParentID，二者合起来把调用树拼出来。" />

<Predict no="80-2" q="代码 2.2 里对一个完全没有 traceparent 头的 http.Header 调用 Extract，返回什么？" :options='["自动生成一个新 trace-id","返回 ok=false，由调用方决定是否开新 Trace","直接 panic","返回全零的 trace-id"]' answer="1" output="无头时 ok: false" explain="Extract 严格校验分段数与各段长度，缺头或格式不合法就返回 ok=false；提取层不臆造 ID，是否新建 Trace 是上层的职责。" />

## 一页纸速记
<KnowledgeCard :points='["Trace 是全链路唯一 ID，Span 是一次操作并带 ParentID，context 在进程内串起父子关系","Inject 把上下文写进 traceparent 头，Extract 从对端还原；两动作缺一，链路就断在那一跳","W3C traceparent 格式为 00-TraceID-SpanID-flags，长度分别固定为 32 与 16 位十六进制","头部采样按 TraceID 确定性判定并随标志位继承；日志统一带 trace_id 才能与链路互跳"]' :pitfalls='["起 goroutine 或发消息时不传带 Span 的 ctx，链路被切成两段","Span 名里放用户 ID 等高基数字段，导致时间序列爆炸","baggage 里塞敏感信息，随每一跳扩散造成泄露"]' summary="一句话记住：TraceID 是通行证，Inject 与 Extract 是出入境盖章，Span 把经过的每一站连成一棵树。" />

## 小结与下一讲

- **模型**：TraceID 贯穿全链路，Span 记父子与耗时，context 在进程内传递，调用树由此拼成。
- **传播**：W3C traceparent 经 Inject 写入、Extract 还原，HTTP 头与 gRPC metadata 只是不同 carrier。
- **采样与关联**：头部采样确定性且随标志继承，日志统一带 trace_id，链路与日志才能互相跳转。

[下一讲：Prometheus / Grafana 与 RED 指标](/microservice/metrics)
