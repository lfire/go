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

## 4. 工程实践

真实 OpenTelemetry 里，这些能力落在几个标准件上：`TracerProvider` 负责创建 Tracer 与配置采样、导出器；`otel.GetTextMapPropagator().Inject(ctx, carrier)` 与 `.Extract(ctx, carrier)` 对应本讲的 Inject/Extract，只是 carrier 泛化成了 HTTP header 或 gRPC metadata；`otelhttp` 与 `otelgrpc` 提供自动插桩，把进出站传播都包好了。理解标准库版本后，换 API 只是换个名字。

<Compare :panels='[{"label":"各打各的日志","code":"log.Printf(order failed)\nlog.Printf(payment failed)","note":"两个服务的日志没有共同 key，排查只能靠时间戳硬凑，跨服务问题几乎查不动"},{"label":"统一带 trace_id","code":"log.Printf(trace_id=%s failed, tid)","note":"一条 TraceID 把网关、订单、支付全串起来，日志与 Span 可互相跳转"}]' title="日志能不能串起一次请求" />

落地时还有几条经验。采样率按流量与成本调，错误与慢请求可以做尾部补采，避免「出事的那条恰好没采到」。Span 命名用低基数、语义化的名字（如 `GET /order` 或 `db.query`），别把用户 ID、订单号塞进 Span 名，否则时间序列会爆炸；这类高基数字段放 attributes。每个 Span 至少打上 `rpc.method`、`http.status_code`、`error` 三类属性，排查才有抓手。日志里统一输出 trace_id 与 span_id，配合第 55 讲的结构化日志，就能从一条日志直接跳到整条调用树。最后记得在 exit span 里 `End()` 并 `defer`，漏了上报会造成链路缺口，看起来就像某个服务凭空消失。

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
