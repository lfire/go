---
title: 第 118 讲 · 实战 12 · LLM 网关与流式推理管道
description: 用 Provider 接口适配多家大模型，实现 SSE 流式转发、令牌限流、超时重试与成本可观测
---

<StageHeader stage="L10 · 工程实战" level="★★★★☆" time="70 分钟" goal="把 Go 的并发优势用在 AI 工程" prereq="第 117 讲 · Kubernetes Operator 与可观测平台" />

# 第 118 讲 · 实战 12 · LLM 网关与流式推理管道

> 教程的最后一个项目回到 Go 最擅长的领域：**大量长连接、高并发、低延迟的字节搬运**。多家大模型供应商的流式协议各不相同——文本字段名不一样、结束标志不一样、用量上报不一样，而业务侧只想调一个接口。网关要把这些差异吃掉，再统一做鉴权、限流、计费与可观测。真正的难点不是转发一次 HTTP 请求，而是**把上游的 token 一个一个、不缓冲、不攒批地实时送到客户端**，并且在客户端断开时立刻停掉上游，别让钱白烧。

## 这一讲你会得到什么

<CheckList id="L118-goal" title="本讲目标" :items='["用 Provider 接口加适配器屏蔽多供应商差异，做到开闭原则","实现真正的 SSE 流式转发：逐块 Flush、禁代理缓冲、感知断开","讲清超时、重试、错误事件三者在流式场景下的特殊边界","用令牌桶按 API Key 限流，并按输出量做成本与可观测统计"]' />

## 项目目标与约束

- **目标**：一个统一入口，按请求里的 `provider` 选择供应商；把各家流式响应归一成统一的 `Chunk`；以 SSE 实时转发给客户端。
- **约束**：只用标准库（不引第三方 SDK）；供应商地址与密钥由配置注入；**首 token 延迟**是核心指标，绝不能等整段生成完再返回。
- **明确边界**：示例里的 `context.WithTimeout` 覆盖整个流，会截断超过 30 秒的长回答；生产应区分「首字节超时」与「块间超时」，本讲在失败路径里讲清代价。

<Term word="SSE" text="Server-Sent Events，基于 HTTP 的单向流式协议，每条消息是 data: 开头的文本块，天然适合逐 token 推送" />

## 架构蓝图

<ConceptDiagram type="gateway" title="LLM 网关：鉴权与限流 → 供应商路由 → 协议适配 → SSE 转发 → 计费" caption="Provider 接口把「选哪家」与「怎么转发」彻底解耦" />

<ConceptDiagram type="architecture-layer" title="分层：HTTP Gateway → Provider 适配层 → 上游 SSE → 统一 Chunk 流" caption="新增一家供应商只需实现 opener 与 parse，调用方与转发层一行不改" />

<FlowTimeline direction="horizontal" :steps='[
  {"title":"请求进入","desc":"校验 API Key、按配额限流","tone":"blue"},
  {"title":"路由供应商","desc":"Registry.Get 选出 Provider","tone":"green"},
  {"title":"发起上游","desc":"httpOpener 带 Accept 请求 SSE","tone":"green"},
  {"title":"解析归一","desc":"parseOpenAI 或 parseAnthropic 产出 Chunk","tone":"orange"},
  {"title":"逐块转发","desc":"data 帧加 Flush，立刻送到客户端","tone":"orange"},
  {"title":"计费收尾","desc":"累计输出量，记录错误与延迟","tone":"red"}
]' />

## 关键实现

以下代码同属 `package aigw`，分别为 `provider.go`、`adapters.go`、`sse.go`、`gateway.go`、`sse_test.go`，同一目录 `go test ./...` 可编译运行。供应商 endpoint 由配置注入，代码里不写死任何外部地址。

### 1. 统一契约：Chunk、Provider 与 SSE 解析器

```go
// 文件：provider.go
package aigw

import (
	"bufio"
	"context"
	"io"
	"strings"
)

// Chunk 是网关对外的统一流式片段，屏蔽各供应商的字段差异。
type Chunk struct {
	Text  string
	Done  bool
	Usage int // 累计消耗 token 数（若供应商提供）
}

// ChatRequest 是网关统一入参。
type ChatRequest struct {
	Model    string
	Messages []string
}

// Provider 是多供应商适配的核心接口：把「一次对话」抽象成统一的块流。
type Provider interface {
	Name() string
	Stream(ctx context.Context, req ChatRequest, emit func(Chunk) error) error
}

// SSEParser 提取上游 Server-Sent Events 的 data 行，供各适配器复用。
// 单行可能很长，因此放大 Scanner 缓冲，避免 token 稍长就被截断。
func SSEParser(r io.Reader, onData func(string) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // 忽略 event、id、retry 与心跳注释行
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if err := onData(payload); err != nil {
			return err
		}
	}
	return sc.Err()
}
```

### 2. 多供应商适配：opener 与 parser 分离

```go
// 文件：adapters.go
package aigw

import (
	"context"
	"encoding/json"
	"io"
)

// 编译期断言：适配器必须满足统一契约。
var _ Provider = (*adapter)(nil)

// opener 由具体供应商实现：发起请求并返回原始 SSE 响应体。
type opener func(ctx context.Context, req ChatRequest) (io.ReadCloser, error)

// adapter 把「取流 + 解析」组装成一个 Provider，是适配器模式的骨架：
// 新增供应商只需换 open 与 parse，Stream 逻辑完全复用。
type adapter struct {
	name  string
	open  opener
	parse func(io.Reader, func(Chunk) error) error
}

func (a *adapter) Name() string { return a.name }

func (a *adapter) Stream(ctx context.Context, req ChatRequest, emit func(Chunk) error) error {
	body, err := a.open(ctx, req)
	if err != nil {
		return err
	}
	defer body.Close()
	return a.parse(body, emit)
}

// parseOpenAI 把 OpenAI 风格的 data 行转成统一 Chunk。
func parseOpenAI(r io.Reader, emit func(Chunk) error) error {
	return SSEParser(r, func(data string) error {
		if data == "[DONE]" {
			return emit(Chunk{Done: true})
		}
		var v struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &v); err != nil {
			return nil // 忽略无法解析的心跳帧，不让一条脏数据掐断整条流
		}
		if len(v.Choices) > 0 && v.Choices[0].Delta.Content != "" {
			return emit(Chunk{Text: v.Choices[0].Delta.Content})
		}
		return nil
	})
}

// parseAnthropic 把 content_block_delta 事件转成同一个 Chunk。
func parseAnthropic(r io.Reader, emit func(Chunk) error) error {
	return SSEParser(r, func(data string) error {
		var v struct {
			Type  string `json:"type"`
			Delta struct {
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &v); err != nil {
			return nil
		}
		if v.Type == "content_block_delta" && v.Delta.Text != "" {
			return emit(Chunk{Text: v.Delta.Text})
		}
		if v.Type == "message_stop" {
			return emit(Chunk{Done: true})
		}
		return nil
	})
}
```

### 3. SSE 流式转发：不缓冲、逐块刷出

```go
// 文件：sse.go
package aigw

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// StreamToClient 把统一 Chunk 流以 SSE 协议写给下游。
// 关键点：正确响应头、每块立即 Flush、监听 ctx 感知客户端断开。
func StreamToClient(w http.ResponseWriter, r *http.Request, stream func(emit func(Chunk) error) error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // 禁止反向代理缓冲，保证逐块到达
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	err := stream(func(c Chunk) error {
		select {
		case <-r.Context().Done():
			return r.Context().Err() // 客户端断开，立刻停止上游，省钱省连接
		default:
		}
		payload, _ := json.Marshal(map[string]any{"text": c.Text, "done": c.Done})
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})
	if err != nil {
		// 响应头已经发出，不能再改状态码，只能写一个 error 事件让前端感知。
		fmt.Fprintf(w, "event: error\ndata: %q\n\n", err.Error())
		flusher.Flush()
	}
}
```

### 4. 网关：注册、鉴权、限流与成本统计

```go
// 文件：gateway.go
package aigw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Registry 按供应商名注册 Provider，新增一家只需注册，不改调用方。
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

func NewRegistry(providers ...Provider) *Registry {
	m := make(map[string]Provider, len(providers))
	for _, p := range providers {
		m[p.Name()] = p
	}
	return &Registry{providers: m}
}

func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

// httpOpener 构造一个真实的流式请求，交给 adapter 做协议适配。
func httpOpener(endpoint, apiKey string, makeBody func(ChatRequest) string) opener {
	return func(ctx context.Context, req ChatRequest) (io.ReadCloser, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
			bytes.NewBufferString(makeBody(req)))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
		}
		return resp.Body, nil
	}
}

// DefaultRegistry 装配两家供应商；地址与密钥全部由配置注入（第 56 讲）。
func DefaultRegistry(openaiEndpoint, openaiKey, anthropicEndpoint, anthropicKey string) *Registry {
	makeBody := func(r ChatRequest) string {
		b, _ := json.Marshal(map[string]any{"model": r.Model, "stream": true})
		return string(b)
	}
	openAI := &adapter{
		name:  "openai",
		open:  httpOpener(openaiEndpoint, openaiKey, makeBody),
		parse: parseOpenAI,
	}
	anthropic := &adapter{
		name:  "anthropic",
		open:  httpOpener(anthropicEndpoint, anthropicKey, makeBody),
		parse: parseAnthropic,
	}
	return NewRegistry(openAI, anthropic)
}

// Gateway 聚合鉴权、限流、超时、供应商选择与成本统计。
type Gateway struct {
	registry *Registry
	limit    *bucket
	keys     map[string]bool
	tokens   atomic.Int64 // 累计输出量，用于成本与可观测
}

func NewGateway(reg *Registry, qps float64, keys ...string) *Gateway {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return &Gateway{registry: reg, limit: newBucket(qps, qps), keys: m}
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("X-API-Key")
	if !g.keys[apiKey] {
		http.Error(w, "invalid api key", http.StatusUnauthorized)
		return
	}
	if !g.limit.allow() {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	var body struct {
		Provider string   `json:"provider"`
		Model    string   `json:"model"`
		Messages []string `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p, ok := g.registry.Get(body.Provider)
	if !ok {
		http.Error(w, "unknown provider", http.StatusBadRequest)
		return
	}
	// 这里用整体超时保护「等到第一个 token」；生产应改为首字节超时加块间超时。
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	req := ChatRequest{Model: body.Model, Messages: body.Messages}
	StreamToClient(w, r, func(emit func(Chunk) error) error {
		return p.Stream(ctx, req, func(c Chunk) error {
			g.tokens.Add(int64(len([]rune(c.Text)))) // 粗略估算，生产用供应商的 usage 字段
			return emit(c)
		})
	})
}

// bucket 是标准库实现的令牌桶，按网关实例做整体配额。
type bucket struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
	rate   float64
	cap    float64
}

func newBucket(rate, cap float64) *bucket {
	return &bucket{tokens: cap, last: time.Now(), rate: rate, cap: cap}
}

func (b *bucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.cap {
		b.tokens = b.cap
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
```

### 5. 解析归一化测试

```go
// 文件：sse_test.go
package aigw

import (
	"strings"
	"testing"
)

func TestParseOpenAI(t *testing.T) {
	upstream := strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n" +
			"data: [DONE]\n\n")
	var got string
	var done bool
	err := parseOpenAI(upstream, func(c Chunk) error {
		got += c.Text
		if c.Done {
			done = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "你好" || !done {
		t.Fatalf("got=%q done=%v", got, done)
	}
}
```

<CodeTabs title="两家供应商的字段差异" :tabs='[{"label":"OpenAI 风格","code":"choices[0].delta.content","note":"文本字段路径，结束标志是 [DONE] 数据块"},{"label":"Anthropic 风格","code":"delta.text","note":"文本字段路径，结束标志是 message_stop 事件"}]' />

| 供应商 | 文本字段 | 结束标志 | 适配方式 |
| --- | --- | --- | --- |
| OpenAI 风格 | `choices[0].delta.content` | `[DONE]` | `parseOpenAI` |
| Anthropic 风格 | `delta.text` | `message_stop` | `parseAnthropic` |
| 网关对外 | `Chunk.Text` | `Chunk.Done` | `StreamToClient` |

## 深入一步：流式转发真正难在哪

把响应体 `io.Copy` 到 `ResponseWriter` 是转发，但不是**流式**转发。只要中间有任何一层攒批——`bufio.Writer` 没 Flush、反向代理缓冲、或者先把整个 body 读进 `[]byte`——首 token 延迟就会退化成整段生成时间，长回答还会把内存吃满。

<Callout type="tip" title="四个缺一不可的细节" text="Content-Type 必须是 text/event-stream；每写一个 data 块就立刻 Flush；设置 X-Accel-Buffering 为 no 让 Nginx 之类不要缓冲；在每个块上检查 r.Context 以便客户端一断开就停止上游。少任何一个，用户看到的效果都是「卡了很久然后一次性蹦出全文」。" />

<Term word="首 token 延迟" text="TTFT，从请求发出到收到第一个 token 的时间，是流式体验最关键的指标，与总生成时长无关" />

`Provider` 只暴露 `Stream(ctx, req, emit)` 一个方法，是刻意的窄接口：`emit` 是一个回调，上游推一块、下游就转一块，中间不落地、不排队。新增供应商时只写 `opener`（怎么发请求）与 `parse`（怎么把 data 行映射成 `Chunk`），`Gateway` 与 `StreamToClient` 完全不动——这就是开闭原则。

## 失败路径与边界条件

| 失败场景 | 后果 | 处理 |
| --- | --- | --- |
| 上游 5xx 或超时 | 客户端拿到空流 | 流未开始前可重试；已开始则只能发 error 事件 |
| 连接建立后才失败 | 状态码已发出，不能改 | 写 `event: error` 事件，前端据此提示 |
| 客户端断开 | 上游还在生成，继续计费 | 每块检查 `r.Context().Done()`，取消会顺 `ctx` 传到 `http.NewRequestWithContext` |
| 反代缓冲 | 逐块变成一次性到达 | `X-Accel-Buffering: no` + 逐块 Flush |
| 流中途重试 | 输出重复、账单翻倍 | 重试只能用于首字节之前，且要有幂等键 |
| 脏数据帧 | 一条坏 JSON 掐断整条流 | 解析失败返回 nil 跳过，不让单帧毁掉全局 |
| token 估算不准 | 成本与配额失真 | 优先采信供应商的 `usage` 字段，`len([]rune)` 仅兜底 |
| 供应商 429 | 全站被一家拖垮 | 透传限流、按供应商熔断与降级（第 79 讲） |

<Callout type="warn" title="超时不能一把梭" text="示例用一个 30 秒的 context 包住整个流，长回答会被硬截断。生产要拆成两段：首字节超时（例如 10 秒内没出第一个 token 就判失败）与块间超时（每收到一块就重置计时器）。把二者混成一个整体超时，是流式网关最常见的线上事故。" />

## 测试与压测思路

- **解析归一**：`TestParseOpenAI` 用 `strings.NewReader` 造一段上游流，断言中文逐块拼接与 `[DONE]`；再补一条 Anthropic 风格的用例验证同一 `Chunk` 输出。
- **假上游**：用 `httptest.NewServer` 按块写 SSE，构造一个 `adapter` 指向它，断言 `Stream` 的块顺序与结束标志。
- **断开取消**：让下游提前取消 `context`，断言 `emit` 返回的错误能沿 `ctx` 传到上游请求并停止读取。
- **压测**：模拟大量并发长连接，观察 goroutine 数与内存（每连接一个）；重点测首 token 延迟分位，而不是总吞吐。
- **故障注入**：上游中途断流、返回 429、发一帧坏 JSON，验证网关不 panic、能发 error 事件、且不留悬挂连接。

## 工程实践 / 真实场景

生产网关还会叠几层能力：按 API Key / 租户做配额与并发上限；用连接池与 `http.Transport` 复用上游连接；把 `trace_id` 贯穿请求、供应商调用与计费记录；对非流式请求做响应缓存与语义缓存；把供应商的错误率、TTFT、输出 token 数做成 RED 指标大盘（第 81 讲），再配熔断与降级，避免一家供应商抖动拖垮全站。成本侧要以供应商返回的 `usage` 为准对账，网关自己的估算只用于实时限额。

## 可继续扩展的方向

- 首字节超时与块间超时分离，并加请求级重试预算。
- 多供应商熔断、自动降级与按负载路由。
- 精确计费（采信 `usage`）、语义缓存与 token 级配额。
- 把 SSE 换成 WebSocket 以承载双向交互，或支持工具调用、多模态事件。

## 动手练一练

<Exercise title="实现首字节超时与块间超时" task="当前一个 30 秒整体超时会截断长回答。请把超时拆成两段：首字节超时 10 秒、之后每收到一块重置计时器。写出关键结构与判断逻辑。" :hints='["用一个定时器加 channel，每收到一块就 Reset","首字节之前用一个 context.WithTimeout，收到第一块后取消它","两条路径都要能因为客户端断开而立刻退出"]' solution="定义 streamGuard 持有 firstTimeout 与 idleTimeout、一个 time.Timer 与 done channel；在 emit 回调里，第一次收到块时取消首字节 ctx 并启动 idle Timer，之后每次收到块调用 timer.Reset(idleTimeout)；另起 goroutine 同时监听 timer.C、r.Context().Done() 与首字节超时，一旦触发就 cancel 上游 ctx 并关闭 done。Stream 循环在每个块前 select 检查 done，命中就返回错误。测试覆盖「10 秒无块失败」「有块持续到达不被截断」「客户端断开立即停」。" explain="流式超时的本质是「允许总时长很长，但不允许长时间没有进展」。把整体超时换成空闲超时，长回答才能既不被误杀、又不会永久挂住。" />

## 随堂检测

<Quiz no="118-1" q="为什么流式转发必须每块 Flush，而不能攒批发送？" :options='["为了少写几行代码","攒批会让首 token 延迟退化成整段生成时间，并可能吃满内存","HTTP 不允许攒批","Go 的 ResponseWriter 默认自动 Flush"]' answer="1" explain="攒批把流式退化成一次性响应，TTFT 变成总生成时长，长回答还会把整段内容留在内存里。逐块 Flush 加禁用反代缓冲才是真流式。" />

<Predict no="118-2" q="上游已经返回 200 并开始写 data 块，此时供应商中途断流，网关能做什么？" :options='["把状态码改成 500","重新发起一次请求并续上输出","写一个 event: error 事件后结束，不能改已发出的状态码","静默结束，什么都不发"]' answer="2" output="写一个 event: error 事件后结束，不能改已发出的状态码" explain="响应头一旦发出就无法回退。只能在流内写一个 error 事件让前端感知；流中途重试会导致输出重复与账单翻倍，因此重试只允许发生在首字节之前。" />

<Quiz no="118-3" q="新增一家大模型供应商，最理想的改动范围是？" :options='["改 Gateway、改 StreamToClient、改所有客户端","只新增一个 opener 与一个 parse 并注册进 Registry","必须重写 Provider 接口","只能改前端"]' answer="1" explain="Provider 是窄接口，adapter 把「取流」与「解析」参数化。新增供应商只需实现 opener 与 parse 并注册，网关与转发层完全不动，这正是开闭原则。" />

## 一页纸速记

<KnowledgeCard :points='["Provider 用 Stream 加 emit 回调做窄接口，新增供应商只换 opener 与 parse","统一 Chunk 屏蔽各家字段差异，结束标志归一为 Done","SSE 转发四要素：正确 Content-Type、逐块 Flush、禁反代缓冲、感知断开","客户端断开要靠 context 一路传到上游请求，否则继续烧钱","重试只能用于首字节之前；流中途失败只能发 error 事件"]' :pitfalls='["攒批或缓冲响应，把流式做成一次性返回","用一个整体超时包住整条流，长回答被硬截断","流中途重试造成输出重复与账单翻倍","客户端断开没取消上游，长连接一直计费","把字符数当精确 token 计费，成本估算失真"]' summary="LLM 网关的价值在于把多供应商差异收进一层适配，并用 Go 的长连接能力把每个 token 实时、可靠、可计量地送到客户端。" />

## 小结与下一讲

1. 用 `Provider` 窄接口 + `adapter`（`opener` 与 `parse` 分离）适配多家供应商，统一成 `Chunk`，新增一家只需注册。
2. `StreamToClient` 给出真正的 SSE 转发：逐块 `Flush`、禁用反代缓冲、每块检查 `r.Context()` 以在客户端断开时取消上游；并讲清「已发头之后不能改状态码」与「重试只能在前」两条边界。
3. 网关把鉴权、令牌桶限流、供应商路由、成本统计收成一处，配 `httptest` 假上游与故障注入测试，指出整体超时这一处必须修的取舍。

到这里，L10 工程实战的十二个项目全部走完：从命令行工具到网络服务、从单机 KV 到分布式存储、从电商秒杀到云原生 Operator，最后落在 AI 流式网关。贯穿始终的是同一套方法——**先定契约与边界，再挑实现，最后用失败路径、测试与压测把它打磨到能上线**。接下来该把这些知识真正用起来了。

[下一讲：动手实践 · 结课与综合练习](/practice/)
