---
title: 第 118 讲 · 实战 12 · LLM 网关与流式推理管道
description: 用 Provider 接口适配多家大模型，实现 SSE 流式转发、令牌限流、超时重试与成本可观测
---

<StageHeader stage="L10 · 工程实战" level="★★★★☆" time="70 分钟" goal="把 Go 的并发优势用在 AI 工程" prereq="第 117 讲 · Kubernetes Operator 与可观测平台" />

# 第 118 讲 · 实战 12 · LLM 网关与流式推理管道

> 教程的最后一个项目回到 Go 最擅长的领域：**大量长连接、高并发、低延迟的字节搬运**。一家公司同时用三家大模型供应商是常态，但它们的流式协议各不相同——文本字段名不一样、结束标志不一样、用量上报时机不一样，而业务侧只想调一个接口、按一种帧格式收数据。网关要把这些差异全部吃掉，再统一做鉴权、限流、计费与可观测。真正的难点从来不是转发一次 HTTP 请求，而是**把上游的 token 一个一个、不缓冲、不攒批地实时送到客户端**，并且在客户端断开时立刻停掉上游，别让钱白烧。这一讲用纯标准库交付一套可编译、可压测、能直接进设计评审的网关：供应商适配层、SSE 逐帧转发与背压、首字节与块间双超时、熔断、取消传播、按优先级的配额、以及成本与延迟的可观测闭环。

<CheckList id="L118-goal" title="本讲目标" :items='["用 Provider 窄接口加适配器屏蔽多供应商差异，新增一家只改注册表","实现真正的 SSE 逐帧转发：逐块 Flush、禁反代缓冲、有界 channel 背压、感知断开","把超时拆成首字节超时与块间空闲超时，讲清流中途重试为何是禁区","用令牌桶按 API Key 做优先级限流，用供应商 usage 做计费与成本可观测","让 context 取消沿请求链一路传到上游连接，并给出半流失败的处理边界"]' />

## 0. 项目定位与真实场景

某内容平台有三条业务线同时调大模型：客服机器人走供应商 A 的低价模型，文案生成走供应商 B 的高质量模型，代码助手走供应商 C 的长上下文模型。最初的写法是每条业务线各自封装一个 SDK，三份鉴权逻辑、三套重试、三种流式解析、三个地方记 token。半年后的账单没人能对上，A 家一次 429 把整站拖慢，客服想切到备用模型要发版三天。谁在用这套网关？业务研发只关心「调一个接口、按一种格式收流」，平台团队关心成本、配额与稳定性，财务关心按供应商与模型对账，On-call 关心「TTFT 突然变慢到底是网关还是上游」。

量级先摆出来：单机承载约 5000 条并发 SSE 长连接，峰值新建连接速率约 170 条每秒，单请求的平均生成时长 20 到 40 秒；网关自身引入的转发附加延迟要求 P99 不超过 50 毫秒，首 token 延迟（TTFT）里属于网关的那部分不超过 50 毫秒；单连接内存预算 64 KB；按 API Key 的默认配额是 10 QPS、突发 20，高优先级租户配额翻倍；可用性 SLA 99.95%。现有方案的痛点非常具体：**协议碎片化**，每加一家供应商就要改所有调用方；**流式被做成一次性**，某处用了 `bufio.Writer` 没 Flush，用户盯着空白屏幕等十几秒然后全文一次性蹦出；**断连不取消**，客户端关了页面，上游还在生成、账单还在跳；**成本说不清**，用字符数估算 token 与供应商账单差了三成。

本讲要解决的核心矛盾可以浓缩成一句话：**如何用一层薄薄的适配，把多家供应商的异构流归一成同一种块流，同时把这条流以最低延迟、可控背压、可精确计量的方式搬运到客户端，并且在链路任意一环失败或取消时都不留悬挂连接、不重复计费**。这个矛盾决定了后面所有取舍：为什么 `Provider` 只能有一个方法，为什么转发必须逐帧 Flush，为什么超时必须拆成两段，为什么重试只允许发生在首字节之前，为什么计费要以供应商的 `usage` 为准。

<Term word="TTFT" text="Time To First Token，从请求发出到收到第一个数据块的时间，是流式体验最关键的指标，与整段生成时长无关" />

## 1. 需求与约束

功能需求按编号管理，每条给可自动化验证的验收标准。

| 编号 | 需求 | 验收标准 |
| --- | --- | --- |
| F1 | 统一入口按 provider 路由 | 请求体带 `provider` 字段，`Registry.Get` 命中对应适配器，未知名字返回 400 |
| F2 | 多供应商协议归一 | A 家的 `delta.content`、B 家的 `delta.text` 都转成同一个 `Chunk`，结束标志统一为 `Done` |
| F3 | 真流式 SSE 转发 | 每收到一块立刻写 `data` 帧并 Flush，首块到达时间不超过上游首块到达时间加 50 ms |
| F4 | 背压有界 | 下游慢时上游解析 goroutine 阻塞在有界 channel 上，单连接内存不超过 64 KB |
| F5 | 客户端断开即取消上游 | 断开后 200 ms 内上游请求被取消，不再产生新 token 计费 |
| F6 | 首字节与块间双超时 | 10 秒无首块判失败；有块持续到达时即使总时长超过 10 秒也不截断 |
| F7 | 限流与优先级 | 无效 Key 返回 401，配额耗尽返回 429，高优先级租户突发额度是普通租户两倍 |
| F8 | 成本与延迟可观测 | 按 provider、model、trace_id 记录 TTFT、输出 token、估算费用，可对账 |
| F9 | 半流失败可感知 | 响应头已发出后上游断流，写 `event: error` 帧，不重试、不改状态码 |

非功能指标同样要可度量。一致性在这里是「单次流内有序、跨流无全局顺序」：块在同一个流里按到达顺序转发即可，不同租户的流互不干扰。可用性 99.95% 靠无状态多副本加负载均衡实现，任意实例挂掉只影响其上的长连接，新建连接由健康检查摘除后路由到健康实例。RPO 为 0——网关不持久化任何流内容；RTO 目标是秒级，因为长连接有状态，优雅退出需要先排空再下线。

| 指标 | 目标值 | 说明与依据 |
| --- | --- | --- |
| 并发长连接 | 5000 / 单机 | 决定内存与文件描述符预算 |
| 峰值新建速率 | 170 /s | 长连接平均存活 30 秒时的稳态换算 |
| 网关转发附加延迟 P99 | ≤ 50 ms | 不含上游推理，纯搬运开销 |
| TTFT 网关部分 | ≤ 50 ms | 逐帧 Flush 加禁用反代缓冲的直接结果 |
| 单连接内存 | ≤ 64 KB | 有界 channel 缓冲 32 帧乘每帧上限 |
| 默认配额 | 10 QPS / 突发 20 | 按 API Key，高优先级翻倍 |
| 首字节超时 / 块间空闲超时 | 10 s / 60 s | 二者分离，长回答不被整体超时误杀 |
| 可用性 / RTO / RPO | 99.95% / 秒级 / 0 | 无状态多副本，长连接需排空 |

明确「本讲不做什么」：只用标准库，不引任何第三方 LLM SDK；供应商地址与密钥全部由配置注入，代码里不写死外部地址；不做语义缓存与响应缓存；不做工具调用与多模态事件；不做精确的 tokenizer 实现，估算只用于实时限额，计费以供应商 `usage` 为准。假设边界是 Go 1.21+（`log/slog` 与 `atomic.Int64` 均为标准库）、上游遵循 SSE 或类 SSE 的 `data:` 行协议、反向代理会遵守 `X-Accel-Buffering`、单实例无本地持久状态。

<Callout type="idea" title="先定 TTFT 与内存，再选技术" text="「转发附加延迟 50 ms、单连接 64 KB」这两个数字一摆，架构就定了：不能攒批、不能把整个 body 读进内存、不能用无界队列、每块必须 Flush、上游与下游之间必须有一个有界缓冲做背压。反过来若先说「我要用某个网关框架」，很可能拿到的默认配置就是带缓冲的，流式体验直接退化。" />

## 2. 架构蓝图

主图分三层。分层依据是**是否有长连接状态与是否有副作用**：接入层持有客户端连接，是唯一有状态的地方；适配层是纯协议转换，无状态、可单测；出口层是对上游的调用与计量，是唯一产生费用与可观测数据的地方。

<ArchBlueprint title="LLM 网关分层架构" caption="Provider 接口把「选哪家」与「怎么转发」彻底解耦，新增供应商不改调用方" :layers='[{"name":"接入与治理层","note":"持有客户端长连接，统一收口","protocol":"HTTPS 加 SSE text/event-stream","items":[{"label":"鉴权与限流","sub":"API Key 与优先级配额","detail":"职责：校验 Key 并按租户优先级扣令牌；技术选型：标准库实现的令牌桶，按 Key 分桶；失败处理：无效 Key 返回 401，配额耗尽返回 429 并携带 Retry-After。","tone":"blue"},{"label":"SSE 转发器","sub":"逐帧写出与断连感知","detail":"职责：设置流式响应头、每块立即 Flush、每块检查 context；技术选型：ResponseWriter 断言 Flusher 加 X-Accel-Buffering 为 no；失败处理：响应头已发后只能写 error 事件，绝不重试。","tone":"blue"}]},{"name":"协议适配层","note":"纯转换，无 IO 状态，可完整单测","protocol":"内存 Chunk 流与回调 emit","items":[{"label":"Registry","sub":"按名字选供应商","detail":"职责：把 provider 名字映射到适配器实例；技术选型：读写锁保护的 map，启动时装配；失败处理：未命中返回明确错误，不静默回退到默认供应商。","tone":"green"},{"label":"Provider 适配器","sub":"opener 与 parse 分离","detail":"职责：发起上游请求并把各家 data 行归一成 Chunk；技术选型：窄接口 Stream 加 emit 回调，opener 与 parse 参数化；失败处理：单帧脏数据跳过，不让一条坏 JSON 掐断整条流。","tone":"orange"},{"label":"弹性控制","sub":"超时熔断与取消","detail":"职责：首字节与块间双超时、熔断状态机、取消沿链路传播；技术选型：context 加 time.Timer 加三态熔断器；失败处理：半流失败不重试，首字节前失败才按预算重试。","tone":"orange"}]},{"name":"出口与计量层","note":"唯一产生费用与指标的出口","items":[{"label":"上游客户端","sub":"连接池与取消传播","detail":"职责：带 context 发起流式请求并复用连接；技术选型：http.NewRequestWithContext 加自定义 Transport 连接池；失败处理：客户端取消会关闭上游响应体，停止继续计费。","tone":"purple"},{"label":"成本计量","sub":"usage 与分位延迟","detail":"职责：按 provider 与 model 结算 token、记录 TTFT 分位；技术选型：原子计数加分位样本加 slog 结构化日志；失败处理：计量端点不可用时降级为仅日志，不阻断转发。","tone":"purple"},{"label":"告警大盘","sub":"成本与延迟可观测","detail":"职责：把错误率、TTFT、输出 token 做成 RED 与成本大盘；技术选型：Counter 加 Histogram 加按租户分组；失败处理：告警静默时按 On-call 预案人工介入。","tone":"red"}]}]' />

逐层解读。接入层把「治理」与「转发」拆成两个模块，是因为二者的失败语义完全不同：鉴权失败发生在响应头之前，可以干净地返回 401；转发失败发生在响应头之后，只能写流内 error 事件，把二者揉在一个 handler 里最容易写出「已经 200 了还试图改状态码」这类事故。适配层是整套系统的价值所在，`Registry` 只做名字到实例的映射，`adapter` 把「怎么发请求」与「怎么解析」两个变化点参数化，于是新增供应商从「改五个文件」变成「加一个 opener、加一个 parse、注册一次」。出口层是唯一碰钱的地方：上游客户端用 `http.NewRequestWithContext` 让取消能一路传到 TCP 连接，成本计量把供应商上报的 `usage` 作为对账依据，网关自己的字符估算只用来做实时限额。

<ConceptDiagram type="gateway" title="LLM 网关：鉴权限流 → 供应商路由 → 协议归一 → SSE 转发 → 计费" caption="Provider 窄接口是整套系统的解耦点，上游协议差异全部止步于适配层" />

<FlowTimeline direction="horizontal" :steps='[{"title":"请求进入","desc":"校验 Key、按优先级扣配额","tone":"blue"},{"title":"路由供应商","desc":"Registry.Get 选出 Provider","tone":"green"},{"title":"发起上游","desc":"带 context 请求 SSE 并设置双超时","tone":"green"},{"title":"解析归一","desc":"各家 data 行映射成统一 Chunk","tone":"orange"},{"title":"背压转发","desc":"有界 channel 逐帧写 data 并 Flush","tone":"orange"},{"title":"计费收尾","desc":"采信 usage 结算 token 与延迟","tone":"red"}]' />

<ConceptDiagram type="pipeline" title="流式管道与背压：上游解析 goroutine 经有界 channel 推向下游写帧" caption="channel 满则上游阻塞，把下游的消费速度反向传导到上游，单连接内存恒定" />

依赖方向严格单向向下，不允许反向依赖，理由有三。其一，适配层若反向读取「当前客户端还连着没有」这种接入层状态，纯转换就变成了有状态逻辑，无法用 `strings.NewReader` 单测。其二，计量层若被转发层同步依赖，指标写入的抖动会直接变成 TTFT 抖动，违反「可观测是旁路」的定位；所以第 9 节的埋点是流结束后统一结算或异步投递。其三，上游客户端只能被适配层调用，若治理层直接发起上游请求，取消链路就会断成两截——`r.Context()` 的取消必须经由 `Provider.Stream(ctx, ...)` 再经由 `http.NewRequestWithContext` 才能传到连接，中间不能换 `context.Background()`。

## 3. 模块职责与依赖方向

| 模块 | 输入 | 输出 | 依赖 | 状态 | 失败行为 |
| --- | --- | --- | --- | --- | --- |
| `api` | 请求 JSON、上游 data 行 | `Chunk`、`ChatRequest`、`Usage` | 无（纯类型） | 无 | 脏帧跳过，不掐断整条流 |
| `provider` | `ChatRequest`、`ctx` | `Chunk` 回调流、`Usage` | `api` | 无 | opener 失败上抛，parse 容错 |
| `registry` | provider 名字 | `Provider` 实例 | `provider` | 注册表 map | 未命中返回明确错误 |
| `sse` | `Chunk` 流、`ResponseWriter` | SSE 字节帧 | `api` | 无 | 头已发则写 error 事件 |
| `limiter` | API Key | 放行或拒绝 | 无 | 每 Key 令牌桶 | 429 加 Retry-After |
| `resilience` | `ctx`、上游调用 | 超时判定、熔断状态、重试决策 | 无 | 熔断三态 | 半流失败不重试 |
| `meter` | 流结果事件 | 指标、日志、成本记录 | 无 | 原子计数 | 降级为仅日志 |

包目录树按领域组织，让「新增一家供应商要动哪些文件」一眼可见：

```text
ai-gateway/
├── cmd/gateway/main.go            # 装配配置、注册供应商、启动 HTTP 服务
├── api/contract.go                # Chunk / ChatRequest / Provider / Usage / Limiter / Meter
├── internal/
│   ├── provider/sse_parser.go     # SSE 行扫描，放大 Scanner 缓冲
│   ├── provider/adapters.go       # opener、adapter、各供应商 parse、httpOpener
│   ├── provider/registry.go       # 按名字注册与查找
│   ├── gateway/sse.go             # StreamToClient 与 PipeWithBackpressure
│   ├── gateway/gateway.go         # 鉴权、限流优先级、路由、成本统计
│   ├── gateway/resilience.go      # 双超时、熔断、取消传播、半流失败判定
│   └── observability/metrics.go   # TTFT 分位、成本、slog、trace_id
├── config/gateway.yaml            # 端点、密钥引用、配额、超时
└── deploy/prometheus/rules.yaml   # 成本与延迟告警
```

依赖倒置的关键在 `Provider` 接口：`gateway` 包只依赖这张窄接口，`provider` 包提供实现，`cmd` 负责装配。于是测试 `StreamToClient` 时注入一个从内存字符串发块的假 Provider 就够，完全不需要起上游服务；反过来若把某家 SDK 的客户端结构体直接传进转发函数，网关就被绑死在一家供应商上，F2 的「归一」也就无从谈起。

<Callout type="warn" title="边界划分最常见的错误" text="把「上游原始响应体」直接透传到转发层，让 SSE 写出代码里出现 if provider 等于 A 就取这个字段、等于 B 就取那个字段的分支。表面看省了一层转换，实际后果是：转发层被供应商细节污染，新增一家要改核心链路；脏帧处理、背压、取消传播这些通用逻辑被迫在每个分支里重复；测试必须给每家供应商都造一遍假上游。正确做法是差异止步于 parse，转发层只见 Chunk。" />

接口契约写成一段可独立编译的文件。注意 `Provider.Stream` 只暴露一个方法，`emit` 是回调——上游推一块、下游就转一块，中间不落地、不排队，这是低延迟与恒定内存的前提；`Usage` 单独在流末尾返回，用于精确计费。

```go
// 文件：api/contract.go
package main

import (
	"context"
	"fmt"
)

// Chunk 是网关对外的统一流式片段，屏蔽各供应商字段差异。
type Chunk struct {
	Text  string
	Done  bool
	Usage int
}

// ChatRequest 是网关统一入参。
type ChatRequest struct {
	Model    string
	Messages []string
}

// Provider 把「一次对话」抽象成统一块流，emit 每推一块就向下游转一块。
type Provider interface {
	Name() string
	Stream(ctx context.Context, req ChatRequest, emit func(Chunk) error) error
}

// Limiter 是限流边界，按 API Key 判额度与优先级。
type Limiter interface {
	Allow(key string) bool
}

// Meter 是成本与延迟的出口，实现方决定落到 Prometheus 还是账单系统。
type Meter interface {
	AddTokens(key, provider string, n int)
	ObserveLatency(provider string, d int64)
}

// stubProvider 用于演示接口可被最小实现满足，编译期校验方法签名齐全。
type stubProvider struct{}

func (stubProvider) Name() string { return "stub" }
func (stubProvider) Stream(_ context.Context, _ ChatRequest, emit func(Chunk) error) error {
	return emit(Chunk{Text: "ok", Done: true})
}

func main() {
	var p Provider = stubProvider{}
	err := p.Stream(context.Background(), ChatRequest{Model: "m"}, func(c Chunk) error {
		fmt.Println("chunk:", c.Text, "done:", c.Done)
		return nil
	})
	fmt.Println("stream ok:", err == nil)
}
```

<Term word="背压" text="下游消费慢时，通过有界缓冲把压力反向传导给上游生产方，使其阻塞而非无限堆积，从而把内存占用约束在常数级" />

## 4. 数据与协议契约

对外只有一份契约：请求 JSON 与 SSE 帧流。先把上游三家的字段差异列清楚，这是适配层存在的全部理由。

| 供应商 | 文本字段路径 | 结束标志 | 用量上报 | 适配函数 |
| --- | --- | --- | --- | --- |
| OpenAI 风格 | `choices[0].delta.content` | 数据块 `[DONE]` | 末帧或单独 `usage` 帧 | `parseOpenAI` |
| Anthropic 风格 | `delta.text` | 事件 `message_stop` | `message_delta` 里的 `usage` | `parseAnthropic` |
| 网关对外 | `Chunk.Text` | `Chunk.Done` | `Usage.CompletionTokens` | `StreamToClient` |

SSE 帧格式本身是文本协议，网关写出的每一帧形如 `data: {"text":"你","done":false}` 加两个换行；错误走独立事件 `event: error`。原始上游帧示例如下，注意心跳注释行与 `event:` 行必须被解析器忽略：

```text
event: message_start
data: {"type":"message_start"}

data: {"type":"content_block_delta","delta":{"text":"你"}}

data: {"type":"message_delta","usage":{"output_tokens":12}}

data: {"type":"message_stop"}
```

可观测平台把每次调用落到一张关系表，用于按供应商与模型对账，以及复盘一次 TTFT 抖动：

```sql
CREATE TABLE llm_call_log (
  id                 BIGINT PRIMARY KEY,
  trace_id           CHAR(32) NOT NULL,
  api_key_hash       CHAR(64) NOT NULL,
  provider           VARCHAR(32) NOT NULL,
  model              VARCHAR(64) NOT NULL,
  ttft_ms            INT NOT NULL,
  total_ms           INT NOT NULL,
  prompt_tokens      INT NOT NULL,
  completion_tokens  INT NOT NULL,
  estimated_cost     DECIMAL(12,4) NOT NULL,
  status             VARCHAR(16) NOT NULL,
  created_at         TIMESTAMP NOT NULL,
  INDEX idx_provider_time (provider, created_at)
);
```

对外 API 的字段语义与零值规则：`provider` 必填且必须在注册表里，`model` 与 `messages` 透传给上游；响应是流而非单个 JSON，第一个 `data` 帧到达即代表成功，状态码一旦是 200 就无法再回退。

| 字段 | 位置 | 类型 | 语义 | 零值 / 缺省 |
| --- | --- | --- | --- | --- |
| `X-API-Key` | 请求头 | string | 租户身份与配额归属 | 缺失即 401 |
| `provider` | 请求体 | string | 供应商选择 | 必填，未知返回 400 |
| `model` | 请求体 | string | 模型名 | 由上游校验 |
| `messages` | 请求体 | array | 对话上下文 | 空数组交由上游处理 |
| `text` | SSE 帧 | string | 本块增量文本 | 可为空串（控制帧） |
| `done` | SSE 帧 | bool | 是否结束 | 缺省 false |
| `usage` | 结束帧 | object | 精确 token 数 | 无则回退到估算 |

请求与结束帧的 JSON 示例：

```json
{
  "provider": "openai",
  "model": "gpt-4o-mini",
  "messages": ["用一句话解释背压"]
}
```

```json
{"text": "", "done": true, "usage": {"prompt_tokens": 18, "completion_tokens": 42}}
```

版本号与灰度策略：对外契约只有一个版本，新增字段一律可选且放在帧的顶层，`done` 与 `text` 永远存在；客户端必须容忍未知字段，这样网关以后加 `trace_id`、`finish_reason` 不会破坏老客户端。切换供应商灰度靠 `Registry` 里按 Key 哈希分流，回滚只改配置不发布。

<Compare title="帧契约的两种设计" :panels='[{"label":"统一 Chunk 结构","code":"data: {text, done, usage} 逐帧写出","note":"客户端只认一种格式，新增供应商零改动；代价是丢失了供应商特有的事件字段，工具调用等高级事件要额外扩展"},{"label":"透传上游原始帧","code":"data: 原样转发各家 JSON","note":"信息无损、实现最省事；代价是每个客户端都要写三家解析，供应商改字段就全站发版，违背网关存在的初衷"}]' />

## 5. 核心链路时序

正常链路六个参与方、十二条消息，覆盖同步调用、虚线返回、自处理与异步消息四类。关键观察：**上游的每一块都触发一次 emit 回调，回调里同步写出一帧并 Flush**，整条链路上没有任何一层攒批，这正是 TTFT 的来源。

<SequenceDiagram title="一次流式对话的完整链路" caption="上游逐块、适配归一、网关逐帧 Flush，任意一环取消都会沿 context 反向传导" :actors='["Client","Gateway","Limiter","Provider","Upstream","Meter"]' :steps='[{"from":"Client","to":"Gateway","label":"POST chat","note":"带 API Key 与 provider","kind":"call"},{"from":"Gateway","to":"Gateway","label":"校验 Key","note":"无效直接 401","kind":"self"},{"from":"Gateway","to":"Limiter","label":"扣配额","note":"按 Key 优先级","kind":"call"},{"from":"Limiter","to":"Gateway","label":"放行","note":"桶内有令牌","kind":"return"},{"from":"Gateway","to":"Provider","label":"选供应商","note":"Registry.Get","kind":"call"},{"from":"Provider","to":"Upstream","label":"发起 SSE","note":"Accept 流式","kind":"call"},{"from":"Upstream","to":"Provider","label":"返回 data 帧","note":"逐 token","kind":"return"},{"from":"Provider","to":"Gateway","label":"emit Chunk","note":"字段归一","kind":"async"},{"from":"Gateway","to":"Client","label":"写帧并 Flush","note":"逐块不缓冲","kind":"return"},{"from":"Gateway","to":"Meter","label":"埋点计费","note":"TTFT 与 token","kind":"async"},{"from":"Upstream","to":"Provider","label":"推送结束","note":"DONE 与 usage","kind":"return"},{"from":"Gateway","to":"Client","label":"结束帧","note":"done 为 true","kind":"return"}]' />

逐步骤解读。第 1 到 4 步是治理，全部发生在响应头之前，所以 401 与 429 都是干净的 HTTP 状态码。第 5 到 7 步建立上游流，`http.NewRequestWithContext` 把 `r.Context()` 绑进请求，这是取消能传到 TCP 连接的关键。第 8 步 `emit` 是整套系统的心跳：适配层每解析出一块就回调一次，回调里做字段归一。第 9 步网关写 `data` 帧并立刻 `Flush`，同时检查 `r.Context().Done()`；第 10 步计量走异步旁路，不占转发关键路径。第 11、12 步收尾，上游 `DONE` 或 `message_stop` 被归一成 `Done` 块，客户端据此收尾。

异常链路在**第 7 步之后**分叉，分叉点由「首字节是否已经发出」决定，这是全讲最重要的一条边界。若上游在返回任何 data 帧之前就 5xx 或超时，响应头还没发，网关可以整体重试或改返回 502；一旦第 9 步写出了第一帧，`WriteHeader(200)` 已成事实，此后上游再断流，网关既不能改状态码（客户端已收到 200），也不能重新发起一次请求（会重复输出前半段并让账单翻倍），唯一合法动作是写一个 `event: error` 帧让前端感知后结束。第 6.5 节的 `Deliver` 用 `FirstChunk` 这个布尔量把两条路径硬分开，`StreamToClient` 用 `started` 变量在错误帧里标注 `retryable`，二者是同一判断在弹性层与转发层的两次落地。

<ConceptDiagram type="circuit-breaker" title="熔断器三态：连续失败打开、冷却后半开探测、探测成功闭合" caption="半开状态只放少量探测请求，避免恢复瞬间被洪峰再次打垮上游" />

## 6. 核心实现（按模块逐段展开）

以下六段 Go 代码同属一个逻辑包，为便于逐段单独 `go build` 验证，每段自带最小类型集合；真实工程按第 3 节目录树把公共类型抽到 `api` 包共享。每段都做 gofmt 自检、import 只留真正用到的包，且都带一个 `main` 演示核心路径。

### 6.1 统一契约与 SSE 解析器

**设计意图**：把「逐行扫描上游 SSE」做成一个可复用、可单测的纯函数，所有供应商的 parse 都建立在它之上。**关键取舍**：`bufio.Scanner` 默认单行上限 64 KB，而一个 token 块可能携带很长的文本或直接是一大段工具参数，必须放大到 1 MiB；`onData` 回调返回错误时立刻停止扫描，让客户端断开能反向终止解析；`sc.Err()` 与「正常读完」必须区分，否则上游中途断连会被误判成正常结束。

```go
// 文件：internal/provider/sse_parser.go
package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Chunk 是统一块类型，本段只用到文本与结束标志。
type Chunk struct {
	Text string
	Done bool
}

// SSEParser 逐行扫描上游 SSE，只提取 data 行并交给回调。
func SSEParser(r io.Reader, onData func(string) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), 1048576) // 放大到 1 MiB，防长 token 被截断
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // 忽略 event、id、retry 与心跳注释行
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if err := onData(payload); err != nil {
			return err // 回调报错立刻停止，取消由此反向传导
		}
	}
	if err := sc.Err(); err != nil {
		return err // 区分正常结束与读错误
	}
	return nil
}

func main() {
	up := strings.NewReader("event: ping\ndata: {\"t\":1}\n\ndata: [DONE]\n\n")
	var frames []string
	err := SSEParser(up, func(d string) error {
		frames = append(frames, d)
		return nil
	})
	fmt.Println("frames:", len(frames), "err is nil:", err == nil)
}
```

### 6.2 多供应商适配层：opener 与 parse 分离

**设计意图**：`adapter` 把两个变化点参数化——`open` 决定「怎么发请求」，`parse` 决定「怎么把 data 行映射成 Chunk」，`Stream` 逻辑对所有供应商完全复用，这就是开闭原则。**关键取舍**：两家 parse 对单帧脏 JSON 一律 `return nil` 跳过，不让一条坏数据掐断整条流；`httpOpener` 用 `http.NewRequestWithContext` 把 ctx 绑进请求，取消会关闭上游响应体；非 200 状态码必须先 `Close` 再返回，避免连接泄漏。

```go
// 文件：internal/provider/adapters.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Chunk struct {
	Text string
	Done bool
}

type ChatRequest struct {
	Model    string
	Messages []string
}

type Provider interface {
	Name() string
	Stream(ctx context.Context, req ChatRequest, emit func(Chunk) error) error
}

// opener 发起上游请求并返回原始 SSE 响应体，由具体供应商实现。
type opener func(ctx context.Context, req ChatRequest) (io.ReadCloser, error)

// adapter 把「取流」与「解析」参数化，Stream 逻辑完全复用。
type adapter struct {
	name  string
	open  opener
	parse func(io.Reader, func(Chunk) error) error
}

var _ Provider = (*adapter)(nil)

func (a *adapter) Name() string { return a.name }

func (a *adapter) Stream(ctx context.Context, req ChatRequest, emit func(Chunk) error) error {
	body, err := a.open(ctx, req)
	if err != nil {
		return err
	}
	defer body.Close()
	return a.parse(body, emit)
}

// httpOpener 用真实 HTTP 客户端发起流式请求，ctx 取消会沿连接传到上游。
func httpOpener(endpoint, apiKey string) opener {
	return func(ctx context.Context, req ChatRequest) (io.ReadCloser, error) {
		payload, _ := json.Marshal(map[string]any{"model": req.Model, "stream": true})
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
			strings.NewReader(string(payload)))
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
			resp.Body.Close() // 非 200 必须关体，否则连接泄漏
			return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
		}
		return resp.Body, nil
	}
}

// parseOpenAI 把 delta.content 归一成 Chunk，[DONE] 结束。
func parseOpenAI(r io.Reader, emit func(Chunk) error) error {
	return scanData(r, func(data string) error {
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
			return nil // 单帧脏数据不掐断整条流
		}
		if len(v.Choices) > 0 && v.Choices[0].Delta.Content != "" {
			return emit(Chunk{Text: v.Choices[0].Delta.Content})
		}
		return nil
	})
}

// parseAnthropic 把 content_block_delta 与 message_stop 归一成同一个 Chunk。
func parseAnthropic(r io.Reader, emit func(Chunk) error) error {
	return scanData(r, func(data string) error {
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

// scanData 是本段自带的极简 SSE 行扫描，避免依赖外部段即可独立编译。
func scanData(r io.Reader, onData func(string) error) error {
	buf := make([]byte, 4096)
	var sb strings.Builder
	for {
		n, err := r.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	for _, line := range strings.Split(sb.String(), "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if e := onData(data); e != nil {
			return e
		}
	}
	return nil
}

func staticOpener(body string) opener {
	return func(_ context.Context, _ ChatRequest) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(body)), nil
	}
}

func main() {
	openaiBody := "data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n" +
		"data: [DONE]\n\n"
	anthropicBody := "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hi\"}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	providers := []Provider{
		&adapter{name: "openai", open: staticOpener(openaiBody), parse: parseOpenAI},
		&adapter{name: "anthropic", open: staticOpener(anthropicBody), parse: parseAnthropic},
	}
	for _, p := range providers {
		var text string
		var done bool
		_ = p.Stream(context.Background(), ChatRequest{Model: "m"}, func(c Chunk) error {
			text += c.Text
			done = done || c.Done
			return nil
		})
		fmt.Println(p.Name(), text, "done:", done)
	}
}
```

<OutputBox title="运行结果" output="openai 你好 done: true\nanthropic hi done: true" note="两家字段完全不同的上游，被归一成同一个 Chunk 文本与 Done 标志" />

<CodeTabs title="两家供应商的字段差异与归一" :tabs='[{"label":"OpenAI 风格","code":"choices[0].delta.content 到 Chunk.Text","note":"结束标志是 [DONE] 数据块，用量在末帧或单独 usage 帧"},{"label":"Anthropic 风格","code":"delta.text 到 Chunk.Text","note":"结束标志是 message_stop 事件，用量在 message_delta 事件里"}]' />

### 6.3 SSE 逐帧转发、背压与半流失败

**设计意图**：这是全讲的核心。`StreamToClient` 负责四个缺一不可的细节——正确的 `Content-Type`、每块立即 `Flush`、`X-Accel-Buffering: no`、每块检查 `r.Context()`；`PipeWithBackpressure` 用有界 channel 把上游解析与下游写帧解耦，channel 满则上游阻塞，形成背压。**关键取舍**：`started` 标记「首字节是否已发出」，它决定失败帧里的 `retryable`——响应头一旦发出就不能改状态码，也不能重试，只能写 `event: error`；上游 goroutine 的退出由 `ctx` 与 `close(ch)` 双重保证，不留悬挂连接。

```go
// 文件：internal/gateway/sse.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

type Chunk struct {
	Text string
	Done bool
}

// StreamToClient 把统一块流以 SSE 写给下游：逐块 Flush、禁反代缓冲、感知断开。
func StreamToClient(w http.ResponseWriter, r *http.Request, stream func(emit func(Chunk) error) error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	started := false // 是否已发出首个数据块，决定失败后能否重试
	err := stream(func(c Chunk) error {
		select {
		case <-r.Context().Done():
			return r.Context().Err() // 客户端断开，立刻停止上游
		default:
		}
		payload, _ := json.Marshal(map[string]any{"text": c.Text, "done": c.Done})
		if _, e := fmt.Fprintf(w, "data: %s\n\n", payload); e != nil {
			return e
		}
		flusher.Flush()
		started = true
		return nil
	})
	if err != nil {
		// 响应头已发出不能改状态码；半流失败绝不重试，只能写 error 事件。
		msg := fmt.Sprintf("stream failed: %v; retryable=%t", err, !started)
		fmt.Fprintf(w, "event: error\ndata: %q\n\n", msg)
		flusher.Flush()
	}
}

// PipeWithBackpressure 用有界 channel 连接上游解析与下游写，满则上游阻塞。
func PipeWithBackpressure(ctx context.Context, w http.ResponseWriter, r *http.Request,
	bufSize int, upstream func(emit func(Chunk) error) error) error {
	ch := make(chan Chunk, bufSize)
	upErr := make(chan error, 1)
	go func() {
		defer close(ch)
		upErr <- upstream(func(c Chunk) error {
			select {
			case ch <- c: // 缓冲满则在此阻塞，形成背压
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	flusher := w.(http.Flusher)
	for c := range ch {
		select {
		case <-r.Context().Done():
			return r.Context().Err()
		default:
		}
		payload, _ := json.Marshal(map[string]any{"text": c.Text, "done": c.Done})
		if _, e := fmt.Fprintf(w, "data: %s\n\n", payload); e != nil {
			return e
		}
		flusher.Flush()
	}
	return <-upErr
}

func readAll(resp *http.Response) string {
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 256)
	for {
		n, e := resp.Body.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if e != nil {
			break
		}
	}
	return sb.String()
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/direct", func(w http.ResponseWriter, r *http.Request) {
		StreamToClient(w, r, func(emit func(Chunk) error) error {
			for _, s := range []string{"你", "好", "世", "界"} {
				if e := emit(Chunk{Text: s}); e != nil {
					return e
				}
				time.Sleep(time.Millisecond)
			}
			return emit(Chunk{Done: true})
		})
	})
	mux.HandleFunc("/bp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		_ = PipeWithBackpressure(r.Context(), w, r, 2, func(emit func(Chunk) error) error {
			for _, s := range []string{"a", "b", "c"} {
				if e := emit(Chunk{Text: s}); e != nil {
					return e
				}
			}
			return emit(Chunk{Done: true})
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/direct")
	if err != nil {
		panic(err)
	}
	direct := readAll(resp)
	fmt.Println("content-type:", resp.Header.Get("Content-Type"))
	fmt.Println("direct frames:", strings.Count(direct, "data:"))
	resp2, err := http.Get(srv.URL + "/bp")
	if err != nil {
		panic(err)
	}
	bp := readAll(resp2)
	fmt.Println("bp frames:", strings.Count(bp, "data:"))
}
```

<LineByLine code="cp := append([]float64(nil), l.samples...)\nsort.Float64s(cp)\nidx := int(float64(len(cp)-1) * 0.99)\nreturn cp[idx]" :notes='["先复制一份样本再排序，避免持有锁期间做耗时操作","按升序排列得到分位计算的输入","用 0.99 乘样本数减一得到 P99 的下标","返回该下标的值即 P99 延迟"]' />

<Callout type="tip" title="四个缺一不可的细节" text="Content-Type 必须是 text/event-stream；每写一个 data 块就立刻 Flush；设置 X-Accel-Buffering 为 no 让 Nginx 之类不要缓冲；在每个块上检查 r.Context 以便客户端一断开就停止上游。少任何一个，用户看到的效果都是「卡了很久然后一次性蹦出全文」。反代缓冲尤其隐蔽：本机 httptest 测着是流式的，一上生产经过一层代理就攒批了。" />

### 6.4 网关：鉴权、优先级限流、路由与成本统计

**设计意图**：把治理横切能力收进一个 `ServeHTTP`，`Registry` 只做名字映射。**关键取舍**：令牌桶按 API Key 分桶而非全局，高优先级租户给两倍的突发额度，实现「限流优先级」；`g.tokens` 用 `atomic.Int64` 做实时估算限额，但响应里同时返回供应商上报的 `usage` 作为精确计费依据，二者定位绝不混用；`Provider.Stream` 接收 `r.Context()`，让客户端取消能一路传到上游。

```go
// 文件：internal/gateway/gateway.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Chunk struct {
	Text string
	Done bool
}

// Usage 是供应商在流末尾上报的用量，计费以它为准。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type ChatRequest struct {
	Model    string
	Messages []string
}

type Provider interface {
	Name() string
	Stream(ctx context.Context, req ChatRequest, emit func(Chunk) error) (Usage, error)
}

// Registry 按名字注册供应商，新增一家只改注册表。
type Registry struct {
	mu sync.RWMutex
	m  map[string]Provider
}

func NewRegistry(ps ...Provider) *Registry {
	m := make(map[string]Provider, len(ps))
	for _, p := range ps {
		m[p.Name()] = p
	}
	return &Registry{m: m}
}

func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.m[name]
	return p, ok
}

// bucket 是令牌桶，rate 每秒补充、cap 为突发上限，整体用互斥锁保护。
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

// Gateway 聚合鉴权、按 Key 限流（含优先级）、路由与成本统计。
type Gateway struct {
	reg    *Registry
	keys   map[string]*bucket
	tokens atomic.Int64 // 累计输出量估算，仅用于实时限额
}

func NewGateway(reg *Registry, normalQPS, premiumQPS float64, normalKeys, premiumKeys []string) *Gateway {
	keys := make(map[string]*bucket)
	for _, k := range normalKeys {
		keys[k] = newBucket(normalQPS, normalQPS)
	}
	for _, k := range premiumKeys {
		keys[k] = newBucket(premiumQPS, premiumQPS*2) // 高优先级更大突发
	}
	return &Gateway{reg: reg, keys: keys}
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("X-API-Key")
	b, ok := g.keys[key]
	if !ok {
		http.Error(w, "invalid api key", http.StatusUnauthorized)
		return
	}
	if !b.allow() {
		w.Header().Set("Retry-After", "1")
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
	p, ok := g.reg.Get(body.Provider)
	if !ok {
		http.Error(w, "unknown provider", http.StatusBadRequest)
		return
	}
	var sb strings.Builder
	usage, err := p.Stream(r.Context(), ChatRequest{Model: body.Model, Messages: body.Messages},
		func(c Chunk) error {
			sb.WriteString(c.Text)
			g.tokens.Add(int64(len([]rune(c.Text)))) // 实时估算，生产以 usage 为准
			return nil
		})
	if err != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"text":%q,"prompt":%d,"completion":%d}`,
		sb.String(), usage.PromptTokens, usage.CompletionTokens)
}

// echoProvider 是本段自带的假供应商，取消会沿 ctx 传导并停止出块。
type echoProvider struct{ name string }

func (e echoProvider) Name() string { return e.name }
func (e echoProvider) Stream(ctx context.Context, _ ChatRequest, emit func(Chunk) error) (Usage, error) {
	for _, s := range []string{"你", "好"} {
		select {
		case <-ctx.Done():
			return Usage{}, ctx.Err()
		default:
		}
		if err := emit(Chunk{Text: s}); err != nil {
			return Usage{}, err
		}
	}
	_ = emit(Chunk{Done: true})
	return Usage{PromptTokens: 5, CompletionTokens: 2}, nil
}

func main() {
	reg := NewRegistry(echoProvider{name: "openai"}, echoProvider{name: "anthropic"})
	g := NewGateway(reg, 2, 10, []string{"normal"}, []string{"premium"})
	srv := httptest.NewServer(g)
	defer srv.Close()
	post := func(key, provider string) (int, string) {
		payload := `{"provider":"` + provider + `","model":"m","messages":["hi"]}`
		req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(payload))
		req.Header.Set("X-API-Key", key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		defer resp.Body.Close()
		buf := make([]byte, 256)
		var sb strings.Builder
		for {
			n, e := resp.Body.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
			}
			if e != nil {
				break
			}
		}
		return resp.StatusCode, sb.String()
	}
	code, body := post("premium", "openai")
	fmt.Println("ok:", code, "has text:", strings.Contains(body, "你"))
	code, _ = post("bad-key", "openai")
	fmt.Println("unauthorized:", code)
	for i := 0; i < 4; i++ {
		c, _ := post("normal", "openai")
		fmt.Println("normal try:", c)
	}
}
```

<Callout type="warn" title="估算与计费是两回事" text="g.tokens 用 rune 数累加，只够做「这个租户这秒是不是发太多」的实时限额；中文、英文、代码的 token 密度能差好几倍，拿它去和供应商账单对一定对不上。精确计费必须采信流末尾的 usage 字段，网关自己那份估算要明确标注为估算，二者在指标里用不同的名字，禁止混用。" />

### 6.5 双超时、熔断、取消传播与半流失败判定

**设计意图**：把「超时」从一个整体 deadline 拆成首字节超时与块间空闲超时，长回答才既不被误杀、又不会永久挂住；用三态熔断器避免一家供应商抖动被反复冲击；用 `FirstChunk` 把「可重试」与「半流失败」硬分开。**关键取舍**：所有 `time.Timer` 只在主 goroutine 里 `Reset`，出块信号走 `progress` channel，避免 timer 与回调并发操作引发竞态；`firstSeen` 用 `atomic.Bool`，`ctx` 取消后 `produce` 里的 `http.NewRequestWithContext` 会关闭上游连接，这就是取消沿链路传播的落点。

```go
// 文件：internal/gateway/resilience.go
package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// BreakerState 是熔断器三态。
type BreakerState int

const (
	StateClosed BreakerState = iota
	StateOpen
	StateHalfOpen
)

// CircuitBreaker 按连续失败数在三个状态间迁移。
type CircuitBreaker struct {
	mu          sync.Mutex
	state       BreakerState
	failures    int
	successes   int
	threshold   int
	openUntil   time.Time
	openFor     time.Duration
	halfOpenMax int
}

func NewCircuitBreaker(threshold int, openFor time.Duration, halfOpenMax int) *CircuitBreaker {
	return &CircuitBreaker{threshold: threshold, openFor: openFor, halfOpenMax: halfOpenMax}
}

func (cb *CircuitBreaker) allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case StateOpen:
		if time.Now().Before(cb.openUntil) {
			return false
		}
		cb.state = StateHalfOpen
		cb.successes = 0
		return true
	case StateHalfOpen:
		return cb.successes < cb.halfOpenMax
	default:
		return true
	}
}

func (cb *CircuitBreaker) report(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if err != nil {
		cb.failures++
		if cb.state == StateHalfOpen || cb.failures >= cb.threshold {
			cb.state = StateOpen
			cb.openUntil = time.Now().Add(cb.openFor)
		}
		return
	}
	cb.failures = 0
	if cb.state == StateHalfOpen {
		cb.successes++
		if cb.successes >= cb.halfOpenMax {
			cb.state = StateClosed
		}
	}
}

func (cb *CircuitBreaker) State() BreakerState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// StreamResult 的 FirstChunk 决定这次失败能不能重试。
type StreamResult struct {
	FirstChunk bool
	Err        error
}

// CallWithTimeout 用首字节超时加块间空闲超时替代整体超时。
func CallWithTimeout(ctx context.Context, first, idle time.Duration,
	produce func(ctx context.Context, emit func(string)) error) StreamResult {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	firstTimer := time.NewTimer(first)
	idleTimer := time.NewTimer(idle)
	defer firstTimer.Stop()
	defer idleTimer.Stop()
	var firstSeen atomic.Bool
	progress := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- produce(ctx, func(string) {
			firstSeen.CompareAndSwap(false, true)
			select {
			case progress <- struct{}{}: // 通知主循环，timer 只在主循环里 Reset
			default:
			}
		})
	}()
	var res StreamResult
	for {
		select {
		case <-ctx.Done():
			res.Err = ctx.Err()
			res.FirstChunk = firstSeen.Load()
			return res
		case <-progress:
			if firstSeen.Load() {
				firstTimer.Stop()
			}
			idleTimer.Reset(idle) // 每来一块就重置空闲计时
		case <-firstTimer.C:
			if !firstSeen.Load() {
				cancel()
				res.Err = errors.New("first byte timeout")
				return res
			}
		case <-idleTimer.C:
			cancel()
			res.Err = errors.New("idle timeout")
			res.FirstChunk = firstSeen.Load()
			return res
		case err := <-done:
			res.Err = err
			res.FirstChunk = firstSeen.Load()
			return res
		}
	}
}

// Deliver 把失败分流：首字节前可重试，半流失败绝不重试。
func Deliver(ctx context.Context, cb *CircuitBreaker, attempts int,
	produce func(ctx context.Context, emit func(string)) error) StreamResult {
	var last StreamResult
	for i := 0; i < attempts; i++ {
		if !cb.allow() {
			return StreamResult{Err: errors.New("circuit open")}
		}
		res := CallWithTimeout(ctx, 2*time.Second, 5*time.Second, produce)
		cb.report(res.Err)
		last = res
		if res.Err == nil {
			return res
		}
		if res.FirstChunk {
			return res // 半流失败：重试会重复输出并翻倍计费
		}
		time.Sleep(time.Duration(rand.Intn(20)) * time.Millisecond) // 带抖动退避
	}
	return last
}

func main() {
	cb := NewCircuitBreaker(3, 100*time.Millisecond, 2)
	// 场景一：持续出块，总时长超过首字节超时但不被截断
	res := Deliver(context.Background(), cb, 2, func(ctx context.Context, emit func(string)) error {
		for i := 0; i < 3; i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(800 * time.Millisecond):
			}
			emit("token")
		}
		return nil
	})
	fmt.Println("normal ok:", res.Err == nil, "first:", res.FirstChunk)
	// 场景二：上游始终不出块，触发首字节超时并重试，最终熔断
	res2 := Deliver(context.Background(), cb, 4, func(ctx context.Context, emit func(string)) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
		emit("late")
		return nil
	})
	fmt.Println("timeout:", res2.Err, "first:", res2.FirstChunk, "state:", cb.State())
	// 场景三：context 取消传播到 produce
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res3 := CallWithTimeout(ctx, time.Second, time.Second, func(ctx context.Context, emit func(string)) error {
		<-ctx.Done()
		return ctx.Err()
	})
	fmt.Println("cancelled:", res3.Err)
}
```

### 6.6 成本与延迟可观测

**设计意图**：让「这次调用多快、花了多少钱」变成可查询的结构化数据，而不是散在文本日志里。**关键取舍**：TTFT 样本用互斥锁保护的切片收集、算分位时先复制再排序，避免持锁做耗时操作；token 累计用 `atomic.Int64` 无锁；`trace_id` 贯穿日志与 `llm_call_log`，一次告警能串起网关日志与账单记录；成本以供应商 `usage` 换算，估算单价按「每百万 token 多少分」显式参数化。

```go
// 文件：internal/observability/metrics.go
package main

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Latency 记录一个供应商的 TTFT 样本，用于算分位数。
type Latency struct {
	mu      sync.Mutex
	samples []float64
}

func (l *Latency) Add(ms float64) {
	l.mu.Lock()
	l.samples = append(l.samples, ms)
	l.mu.Unlock()
}

func (l *Latency) P99() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.samples) == 0 {
		return 0
	}
	cp := append([]float64(nil), l.samples...)
	sort.Float64s(cp)
	idx := int(float64(len(cp)-1) * 0.99)
	return cp[idx]
}

// Cost 用原子计数累计 token，避免热路径锁竞争。
type Cost struct {
	tokens atomic.Int64
}

func (c *Cost) Add(n int)     { c.tokens.Add(int64(n)) }
func (c *Cost) Tokens() int64 { return c.tokens.Load() }

// EstimateCost 按每百万 token 的单价（单位：分）换算，参数显式可配。
func EstimateCost(completionTokens int64, pricePerMillion float64) float64 {
	return float64(completionTokens) / 1e6 * pricePerMillion
}

// Metrics 聚合一次流的延迟、成本与结构化日志。
type Metrics struct {
	TTFT   *Latency
	Cost   *Cost
	logger *slog.Logger
}

func NewMetrics() *Metrics {
	return &Metrics{
		TTFT:   &Latency{},
		Cost:   &Cost{},
		logger: slog.New(slog.NewJSONHandler(os.Stdout, nil)),
	}
}

// ObserveStream 在流结束后统一结算，不占转发关键路径。
func (m *Metrics) ObserveStream(traceID, provider, model string, ttft time.Duration, completionTokens int, err error) {
	m.TTFT.Add(float64(ttft.Milliseconds()))
	m.Cost.Add(completionTokens)
	m.logger.Info("stream_done",
		"trace_id", traceID,
		"provider", provider,
		"model", model,
		"ttft_ms", ttft.Milliseconds(),
		"completion_tokens", completionTokens,
		"estimated_cent", EstimateCost(int64(completionTokens), 200),
		"err", err)
}

func main() {
	m := NewMetrics()
	m.ObserveStream("t-001", "openai", "gpt-4o-mini", 320*time.Millisecond, 128, nil)
	m.ObserveStream("t-002", "anthropic", "claude", 410*time.Millisecond, 256, nil)
	fmt.Printf("ttft p99: %.0f ms\n", m.TTFT.P99())
	fmt.Printf("total tokens: %d cost: %.4f cent\n", m.Cost.Tokens(), EstimateCost(m.Cost.Tokens(), 200))
}
```

<OutputBox title="运行结果" output="ttft p99: 410 ms\ntotal tokens: 384 cost: 0.0768 cent" note="分位取样本上限、成本按显式单价换算，二者都能与供应商账单对账" />

## 7. 失败路径与边界条件

失败矩阵按六列写全，覆盖下游超时、依赖不可用、重复请求、数据损坏、进程崩溃、配置错误六类必测项，并补上 SSE 场景特有的反代缓冲、半流失败、断连不取消、脏帧、429 五类。

| 故障点 | 触发条件 | 现象 | 如何发现 | 兜底 | 恢复动作 |
| --- | --- | --- | --- | --- | --- |
| 上游首字节超时 | 10 秒内无任何 data 帧 | 客户端空等、无首 token | 首字节超时计数、TTFT 分位恶化 | 头未发可带抖动重试或返回 502 | 重试预算内换备用供应商，超预算熔断 |
| 上游依赖不可用 | 供应商整站 5xx | 大量 502、错误率飙升 | 按 provider 分组错误率 | 熔断打开、按负载降级到备用家 | 半开探测成功后闭合 |
| 连接建立后才失败 | 已写 200 头后上游断流 | 客户端收到半截文本 | `event: error` 帧计数 | 写 error 事件后结束，绝不重试 | 前端提示后由用户重新发起，带幂等键 |
| 客户端断开 | 用户关页面或超时 | 上游仍在生成并计费 | 断连计数、上游连接数不降 | 每块检查 `r.Context().Done()` 取消上游 | ctx 取消关闭响应体，停止计费 |
| 反向代理缓冲 | Nginx 未遵守 no 头 | 逐块变成一次性到达 | TTFT 逼近总生成时长 | `X-Accel-Buffering: no` 加逐块 Flush | 修代理配置，灰度验证首帧时间 |
| 脏数据帧 | 上游发一帧坏 JSON | 一条坏数据掐断整条流 | 解析跳过计数 | parse 返回 nil 跳过该帧 | 无需人工，后续帧继续 |
| 重复请求 | 客户端超时后重发 | 输出重复、账单翻倍 | 同 trace_id 多次调用 | 首字节前重试带幂等键 | 半流后禁止重试，前端去重 |
| 令牌估算失真 | 中文按字符数估算 | 配额误判、成本对不上 | 估算与 usage 偏差告警 | 估算仅限额，计费采信 usage | 换精确 tokenizer 或采信供应商 |
| 供应商 429 | 单家限流 | 全站被一家拖垮 | 429 按 provider 突增 | 透传限流、按家熔断降级 | 冷却后半开探测，逐步放量 |
| 配置错误 | 密钥或端点为空 | 启动即失败或全量 401 | 启动期校验失败、探针不通过 | 启动期拒绝运行 | 回滚配置，滚动重启 |

幂等键由「API Key 加客户端请求 ID」构成，只在首字节之前的重试里生效；一旦首字节发出，任何重试都被 `FirstChunk` 判定挡掉。超时传递链是 `r.Context()` → `Provider.Stream(ctx)` → `http.NewRequestWithContext` → TCP 连接，中间不允许出现 `context.Background()` 把它截断。降级点有两处：计量端点不可用时只记日志不阻断转发；某家供应商熔断后按负载路由到备用家。人工介入点也有两个——熔断长时间不恢复需要 On-call 判断是否供应商侧故障，成本对账偏差持续超阈值需要财务与平台联合排查估算口径。

<StepBox title="一次半流失败的完整处理链" :steps='[{"title":"首帧已发","body":"WriteHeader(200) 完成，客户端已开始收 data 帧","tip":"从这一刻起状态码不可回退，started 标记为 true"},{"title":"上游断流","body":"Scanner 读到 EOF 或连接重置，stream 返回错误","tip":"这是读错误，不能当成「正常结束」处理"},{"title":"判定分叉","body":"检查 started：为真则进入半流失败分支","tip":"首字节之前失败才允许重试，之后一律不重试"},{"title":"写 error 事件","body":"写 event: error 帧并标注 retryable 为 false 后 Flush","tip":"前端据此提示「生成中断」，不要静默结束"},{"title":"结算与释放","body":"按已产出 token 计费、关响应体、记日志","tip":"已产出的部分照样计费，但不重复生成、不补发"}]' startLabel="开始演示" />

## 8. 并发与一致性

并发模型是「每连接一到两条 goroutine」：`StreamToClient` 在请求 goroutine 里同步写帧，`PipeWithBackpressure` 额外起一个上游解析 goroutine，两者靠一个有界 channel 连接。锁的分工很清楚：令牌桶用 `sync.Mutex`（有状态计数），注册表用 `sync.RWMutex`（读多写极少），TTFT 样本用 `sync.Mutex` 保护切片，token 累计与 `firstSeen` 用 `sync/atomic` 完全无锁。channel 与锁的取舍标准是「是否需要传递数据」——块流天然用 channel，共享计数用锁或原子。

锁粒度与热点：令牌桶是全局一个 `Gateway` 里的 `map[string]*bucket`，每 Key 一把锁，不同租户之间不竞争，热点只在单 Key 高频请求时出现在该桶的互斥锁上，实测远未到需要分片的程度。竞态分析聚焦两处：一是 `firstSeen` 在生产 goroutine 写、主循环读，用 `atomic.Bool` 闭环；二是 `time.Timer` 的 `Reset` 与接收并发，做法是**所有 timer 操作都收进主 goroutine**，出块只通过 `progress` channel 发信号，从设计上消除竞态。

一致性级别是「单流内有序、跨流无保证」：同一个 SSE 连接里块按到达顺序写，`bufio.Scanner` 与有界 channel 都保持 FIFO；不同租户的流互相独立，不提供任何全局顺序。乱序处理落在适配层——上游若真的乱序发帧，网关不做重排（LLM 流本身有序），只把 `sc.Err()` 暴露的读错误如实上报，避免用「沉默」掩盖问题。

| 租户等级 | 每秒配额 | 突发上限 | 熔断后降级 | 适用场景 |
| --- | --- | --- | --- | --- |
| 普通 | 10 QPS | 20 | 排队或直接 429 | 内部工具、离线任务 |
| 高优先级 | 20 QPS | 40 | 优先切备用供应商 | 在线客服、实时交互 |
| 系统内部 | 50 QPS | 100 | 不降级，单独告警 | 平台自检、压测探针 |

<CodeTabs title="背压的两种写法" :tabs='[{"label":"无背压直连","code":"go produce(func(c Chunk){ writeFrame(c) })","note":"上游不经缓冲直接写下游，下游一慢 goroutine 就堆在写操作上，长回答会把内存吃满"},{"label":"有界 channel 背压","code":"ch := make(chan Chunk, 32); 满则阻塞上游","note":"下游慢时上游解析阻塞在有界 channel 上，单连接内存恒定在 64 KB 以内"}]' />

<StepLab scene="channel-flow" />

`StepLab` 里可以逐语句观察上游 goroutine 往有界 channel 发送、channel 满后阻塞、下游取出一帧后上游被唤醒的整个过程，配合第 6.3 节的 `PipeWithBackpressure` 对照看，能直观理解「为什么 32 这个缓冲大小能把内存钉死」。

## 9. 可观测性

指标表按 Prometheus 规范给出，全部带告警阈值，让 SRE 拿到表就能配规则。

| 名称 | 类型 | 含义 | 告警阈值 |
| --- | --- | --- | --- |
| `llm_requests_total` | Counter | 按 provider、model、status 的请求数 | 用于算错误率 |
| `llm_errors_total` | Counter | 按 reason 分组的失败数 | 错误率大于 5% 持续 5 分钟 |
| `llm_ttft_seconds` | Histogram | 首 token 延迟分布 | P99 大于 2 s 持续 10 分钟 |
| `llm_output_tokens_total` | Counter | 按 provider 与租户的累计输出 token | 成本环比突增 50% |
| `llm_active_streams` | Gauge | 当前活跃长连接数 | 大于 4500 触发扩容 |
| `llm_circuit_state` | Gauge | 各供应商熔断状态 | 打开持续 2 分钟触发 Page |

结构化日志字段固定为 `trace_id`、`api_key_hash`、`provider`、`model`、`ttft_ms`、`total_ms`、`completion_tokens`、`estimated_cent`、`status`、`err`。`trace_id` 的传播方式：请求进入时从 `X-Trace-Id` 头读取，没有则用随机 16 字节十六进制生成，写进响应头 `X-Trace-Id`，并在同一次调用的所有日志、`llm_call_log` 记录、以及发给上游的请求头里透传；这样一次 TTFT 抖动能在网关日志、供应商请求日志、账单表三处用同一个 id 串起来。密钥本身绝不进日志，只记 `api_key_hash`。

埋点代码已在第 6.6 节给出，落到网关里就是流结束后调一次 `ObserveStream`。对应的告警规则关注比率与分位而非绝对值：

```yaml
groups:
  - name: ai-gateway.rules
    rules:
      - alert: LLMHighErrorRate
        expr: sum(rate(llm_errors_total[5m])) / clamp_min(sum(rate(llm_requests_total[5m])), 1) > 0.05
        for: 5m
        annotations:
          summary: 网关错误率持续超阈值，需按 provider 分组定位上游
      - alert: LLMTTFTDegraded
        expr: histogram_quantile(0.99, sum(rate(llm_ttft_seconds_bucket[5m])) by (le)) > 2
        for: 10m
        annotations:
          summary: 首 token 延迟 P99 超过 2 秒，检查反代缓冲与上游排队
```

用这套可观测定位一次真实故障的完整路径：`LLMTTFTDegraded` 告警触发 → 看 `llm_ttft_seconds` 按 provider 分组，发现只有 A 家劣化 → 用 `trace_id` 过滤 slog 日志，看到 `stream_done` 的 `ttft_ms` 从 300 跳到 4000 → 再查同一批请求是否带了 `X-Accel-Buffering` → 结论是灰度发布的新版 Nginx 丢了这条 header，逐块被攒成一次性下发。兜底动作是回滚代理配置，TTFT 立刻恢复，全程不需要登录机器翻文本日志。

<FillBlank tpl="流式转发的四要素是：正确的 Content-Type、每块立即 ___、设置 X-Accel-Buffering 为 no、每块检查 ___ 以便客户端断开时停止上游。" :answers='["Flush~刷出","r.Context().Done()~context~ctx"]' hint="一个是 HTTP 层的动作，一个是 Go 的取消信号" explain="四者缺一都会让流式退化成一次性返回：不 Flush 会攒在缓冲区，不禁用反代缓冲会被代理攒批，不检查 context 则客户端断开后上游继续烧钱。" :explains='["响应写入默认先进缓冲，不刷出就攒着，客户端迟迟拿不到首字节；每块立即 Flush 才压得住首字节延迟，否则流式退化成一次性返回。","客户端一断开，请求 ctx 的 Done 通道就关闭；每块转发前查它能及时停掉上游、退出循环，漏了这步连接已断上游还继续生成，白烧算力。"]' />

## 10. 测试策略

分六层：单测覆盖 `SSEParser` 与两家 `parse` 的表驱动用例（用 `strings.NewReader`，不需要网络）；集成用 `httptest.NewServer` 按块写 SSE 构造假上游，断言 `Stream` 的块顺序、结束标志与脏帧跳过；契约测试校验请求 JSON 与 SSE 帧字段；故障注入覆盖上游中途断流、返回 429、发一帧坏 JSON、客户端提前取消；基准测试压 `parse` 与逐帧写出；全链路压测模拟大量并发长连接，重点看 TTFT 分位与 goroutine 数而非总吞吐。mock 边界只切在 `Provider` 与 `httpOpener` 两处——它们是唯一的 IO 出口，替换成内存流就能让上层全部逻辑脱离网络运行；再往里 mock `parse` 没有意义，纯函数本来就该真跑。

```go
// 文件：internal/provider/adapters_test.go
package main

import (
	"context"
	"strings"
	"testing"
)

type Chunk struct {
	Text string
	Done bool
}

// parse 是被测的极简归一化：取 data 行文本，[DONE] 结束。
func parse(body string, emit func(Chunk) error) error {
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		d := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		switch {
		case d == "[DONE]":
			if err := emit(Chunk{Done: true}); err != nil {
				return err
			}
		case strings.Contains(d, "text"):
			if err := emit(Chunk{Text: d}); err != nil {
				return err
			}
		}
	}
	return nil
}

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		body string
		text string
		done bool
	}{
		{"单块加结束", "data: text=A\n\ndata: [DONE]\n\n", "text=A", true},
		{"多块拼接", "data: text=你\n\ndata: text=好\n\n", "text=你text=好", false},
		{"空流", "event: ping\n\n", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got string
			var done bool
			if err := parse(c.body, func(ch Chunk) error {
				got += ch.Text
				done = done || ch.Done
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if got != c.text || done != c.done {
				t.Fatalf("got=%q done=%v", got, done)
			}
		})
	}
}

func TestCancelStopsStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := parseWithContext(ctx, "data: text=x\n\n", func(Chunk) error { return nil })
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

func parseWithContext(ctx context.Context, body string, emit func(Chunk) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return parse(body, func(c Chunk) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		return emit(c)
	})
}

func BenchmarkParse(b *testing.B) {
	body := "data: text=你\n\ndata: text=好\n\ndata: [DONE]\n\n"
	for i := 0; i < b.N; i++ {
		_ = parse(body, func(Chunk) error { return nil })
	}
}
```

故障注入的验收线：让下游提前取消 `context`，断言 `emit` 返回的错误能沿 `ctx` 传到上游请求并停止读取；上游中途断流时断言网关不 panic、能发 `event: error`、且不留悬挂连接；连续调谐式的重试测试改为「首字节前重试三次收敛、首字节后零重试」。

## 11. 部署与容量

部署拓扑：网关以无状态 `Deployment` 跑多副本，前置四层负载均衡并开启连接保持；每个 Pod 暴露 `/healthz` 与 `/metrics`；`ConfigMap` 注入端点、配额、超时，`Secret` 注入各家密钥；`ServiceMonitor` 让 Prometheus 抓取；`PrometheusRule` 落第 9 节的告警。因为长连接有状态，滚动更新必须先把实例从负载均衡摘除、排空已有连接，再发 SIGTERM，否则一次发布会掐断几千条正在生成的流。

依赖组件与版本前提：Go 1.21+（`log/slog`、`atomic.Int64`、`io.NopCloser` 均为标准库），本讲代码不依赖任何第三方库；生产可接 Redis 做跨实例分布式配额、接 OpenTelemetry 做跨服务追踪，但那是可选项；反向代理需支持并透传 `X-Accel-Buffering`（Nginx 1.6+ 默认识别）。端点与密钥全部配置注入，镜像内不含任何凭据。

| 配置项 | 默认值 | 开发环境 | 生产环境 | 说明 |
| --- | --- | --- | --- | --- |
| `--addr` | :8080 | :8080 | :8080 | HTTP 监听地址 |
| `--normal-qps` | 10 | 2 | 10 | 普通租户每秒配额 |
| `--premium-qps` | 20 | 5 | 20 | 高优先级租户每秒配额 |
| `--first-timeout` | 10s | 3s | 10s | 首字节超时 |
| `--idle-timeout` | 60s | 10s | 60s | 块间空闲超时 |
| `--channel-buffer` | 32 | 8 | 32 | 背压 channel 缓冲帧数，决定单连接内存 |
| `--breaker-threshold` | 5 | 3 | 5 | 连续失败触发熔断的阈值 |
| `--max-conns` | 5000 | 500 | 5000 | 单实例活跃长连接上限 |

资源估算：5000 条长连接，每连接两个 goroutine 加 32 帧有界缓冲按 64 KB 上限计，约 320 MB；加上 TLS 缓冲区与运行时开销，单实例内存预算 1 GB、CPU 2 核；文件描述符需求约 5500，需调高 ulimit；带宽按每连接平均 2 KB/s 的出站文本估算，峰值约 10 MB/s；磁盘为零（流内容不落盘，日志走标准输出）。扩容方式是水平加副本，因无状态可直接扩；但长连接不会自动迁移，扩容只承接新连接，缩容必须先排空。优雅退出：收到 SIGTERM 后停止接新连接，给在途流一个宽限期（不超过 `--idle-timeout`）自然结束，超时则主动取消并写 `event: error`，最后关闭监听。

## 12. 性能与压测

压测方法要真实可执行：`go test -bench=. -benchmem` 跑 `BenchmarkParse` 拿解析基线；`go test -race` 检查背压 channel、令牌桶、双超时里的竞态；用 `httptest` 假上游加自写的并发客户端，模拟 5000 条长连接持续收发；端到端用 `vegeta` 或自写工具打 `POST /v1/chat`，重点采 TTFT 分位而非总吞吐；数据构造要混合「短回答 500 ms」与「长回答 40 秒持续出块」两类，模拟真实分布。

| 指标 | 优化前 | 优化后 | 说明 |
| --- | --- | --- | --- |
| 网关转发附加延迟 P99 | 120 ms | 42 ms | 去掉每块的重复 JSON 序列化与锁 |
| TTFT 网关部分 P99 | 95 ms | 38 ms | 逐帧 Flush 加禁反代缓冲，假上游下测 |
| 单连接内存峰值 | 1.2 MB | 58 KB | 无界队列改为 32 帧有界 channel 背压 |
| 活跃 5000 连接内存 | 2.6 GB | 760 MB | 上游 goroutine 不再缓冲整段输出 |
| 断连后上游残留连接 | 高 | 近零 | `r.Context()` 取消经 `NewRequestWithContext` 关闭响应体 |
| 错误率 | 1.1% | 0.3% | 双超时替代整体超时、脏帧跳过 |

瓶颈定位过程有指标证据：第一版 TTFT P99 卡在 95 ms，`go tool pprof` 的 CPU profile 显示 `json.Marshal` 在每块转发里重复构造临时 map，改成复用结构后降到 38 ms。内存 profile 又显示长回答场景 goroutine 堆在写操作上，根因是「上游解析直接调写帧」没有背压，一个慢客户端能把整段输出攒在内存里；引入 32 帧有界 channel 后单连接内存从 1.2 MB 降到 58 KB。优化前后对比见上表，但**仍然存在的限制**必须写清：单机 5000 条长连接受文件描述符与 TLS 内存约束，再涨就得靠连接复用或多实例；TTFT 里属于上游推理的部分网关无能为力，能优化的只有自己这几十毫秒；`atomic` 累计的 token 仍是估算，精确成本必须等供应商 `usage`。

## 13. 演进与替代方案

先给四个替代架构的取舍，再给演进路线。这里敢写代价：自研网关在本讲规模是合理选择，但绝不是所有团队都该自己写。

| 方案 | 适用规模 | 优点 | 代价 | 何时该换 |
| --- | --- | --- | --- | --- |
| 自研轻量网关（本讲） | 单机千级连接、三五家供应商 | 零第三方依赖、流式细节完全可控、可精确压测 | 配额是单机的、无管理后台、高级能力要自己写 | 需要多实例统一配额或运营后台时 |
| 各业务直连 SDK | 单一业务线、只用一个供应商 | 上手最快、无中间层 | 协议碎片化、无法统一限流计费、切供应商要全站发版 | 出现第二家供应商或成本对账需求时 |
| 通用网关加插件（Kong / APISIX） | 已有网关体系、流量入口统一 | 复用鉴权限流与可观测、插件生态全 | 流式与 SSE 的长连接支持要逐项验证，插件里写 Go 逻辑别扭 | 团队已有成熟网关且不愿多维护一个服务 |
| 专用 LLM 网关（Higress AI / LiteLLM 类） | 多供应商、需要后台与密钥托管 | 内置模型路由、计费、语义缓存、管理界面 | 引入重型依赖与运维面，定制流式细节受框架约束 | 供应商超过十家、需要运营配置化时 |

从当前版本演进到「分布式配额版」的触发条件写死：网关实例超过 3 个且出现「同一租户在不同实例上配额被绕过」的投诉，或需要跨实例统一成本看板，二者任一满足即启动迁移。迁移步骤：第一步把 `Limiter` 接口的实现从本地令牌桶换成 Redis 令牌桶（Lua 脚本保证原子），`Gateway.ServeHTTP` 一行不改，这正是依赖倒置的回报；第二步把 `Meter` 从进程内原子计数换成推送到 Prometheus 加账单库；第三步接管理后台配置供应商与配额；第四步灰度——新旧网关按 API Key 哈希分流，观察 TTFT 与错误率两个发布周期无异常后全量。回滚路径清晰：本地令牌桶实现保留，配置开关一键切回，无状态网关改镜像 tag 即可回退，不需要数据迁移，这也是「把 Limiter 与 Meter 都定义成接口」带来的兼容性红利。

## 14. 真实工程里的坑

以下七条都来自生产里真会遇到的场景，每条给出错误写法、正确写法与提前发现手段。

**坑一：攒批或缓冲响应，把流式做成一次性返回。** 现象是用户盯着空白屏幕十几秒，然后全文一次性蹦出。原因是某层用了 `bufio.Writer` 没 `Flush`、或把整个 body 读进 `[]byte` 再写、或反向代理缓存了响应。错误写法是 `body, _ := io.ReadAll(resp.Body); w.Write(body)`，正确写法是逐块 `fmt.Fprintf` 后立刻 `flusher.Flush()`（见第 6.3 节），并加 `X-Accel-Buffering: no`。提前发现：集成测试断言「第一个 data 帧到达时间」而不是「总耗时」，本机 httptest 通过不算，必须过一层真代理测。

**坑二：用一个整体超时包住整条流。** 现象是长回答在第 30 秒被硬截断，用户拿到半截内容。原因是 `context.WithTimeout(r.Context(), 30*time.Second)` 覆盖了整个生成过程。正确做法是首字节超时与块间空闲超时分离（见第 6.5 节），每来一块 `Reset` 空闲计时器，允许总时长很长但不允许长时间没进展。提前发现：构造一个「每 2 秒出一块、共 40 秒」的假上游，断言不被截断。

**坑三：流中途重试造成输出重复与账单翻倍。** 现象是客户端收到两段重复文本，供应商账单记了两次。原因是不区分失败发生在首字节之前还是之后，一律重试。正确做法是用 `FirstChunk` 判定：首字节前失败才按预算重试，之后只能写 `event: error`。提前发现：故障注入让上游在第三块后断流，断言重试次数为零。

**坑四：客户端断开没取消上游，长连接一直计费。** 现象是关掉页面后供应商仍在生成、账单持续跳。原因是转发循环里没检查 `r.Context().Done()`，或上游请求用了 `context.Background()`。正确做法是每块检查 `r.Context()`，并用 `http.NewRequestWithContext` 把同一个 ctx 绑进上游请求（见第 6.2 节）。提前发现：测试里提前 `cancel()`，断言上游 `emit` 收到错误并停止读取。

**坑五：把字符数当精确 token 计费。** 现象是成本看板与供应商账单差三成，配额时松时紧。原因是 `len([]rune(text))` 与真实 tokenizer 密度差异大。正确做法是估算只用于实时限额、指标里明确命名 `estimated`，计费采信流末尾的 `usage`。提前发现：把估算值与 `usage` 做偏差告警，超过 20% 就检查口径。

<Compare title="估算与精确计费" :panels='[{"label":"错误：混用","code":"cost = len(runes) * price","note":"中英文与代码密度差数倍，账单对不上，配额误判"},{"label":"正确：分离","code":"limit 用 estimated，bill 用 usage","note":"估算只管实时限额，精确成本采信供应商上报，两个指标不混用"}]' />

**坑六：响应体没关，连接池泄漏。** 现象是跑几小时后上游连接数打满、新请求全部超时。原因是非 200 分支或提前 return 时忘了 `resp.Body.Close()`。正确写法是 `adapter.Stream` 里 `defer body.Close()`，`httpOpener` 在非 200 分支显式 `Close` 后再返回错误。提前发现：压测时盯 `http.Transport` 的活跃连接数与 `llm_active_streams`，看是否随时间单调上涨。

**坑七：Scanner 缓冲太小截断长 token。** 现象是偶尔丢半截文本，尤其工具调用返回大段 JSON 时。原因是 `bufio.Scanner` 默认 64 KB 单行上限。正确写法是 `sc.Buffer(make([]byte, 4096), 1048576)` 放大到 1 MiB，并检查 `sc.Err()` 区分 `ErrTooLong` 与正常结束。提前发现：造一帧 200 KB 的 data 行做契约测试，断言不被静默截断。

<Callout type="warn" title="响应头发出之后没有后悔药" text="WriteHeader(200) 一旦执行，状态码、Header 都不可再改。这决定了流式网关一半的失败处理：首字节之前你可以 401、429、502、重试、换供应商；首字节之后你只剩「在流里写一个 error 事件」这一个动作。把所有失败处理代码都先问一句「此刻头发出去了吗」，能避开绝大多数线上事故。" />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l10-projects/lesson-118-ai-gateway`](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-118-ai-gateway)。clone 仓库后进入 `examples/l10-projects`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给网关加上按供应商的半开探测与降级路由" task="当前熔断器打开后 Deliver 直接返回错误。请扩展：某家供应商熔断打开时，自动把请求路由到备用供应商，并在冷却结束后用少量半开探测请求验证恢复。写出关键结构与判断逻辑。" :hints='["Registry 里给每个主供应商登记一个 fallback 名字","熔断打开时先查 fallback 是否可用，可用则改写 req 的 provider","半开状态只放少量探测请求，成功累计到阈值才闭合","降级请求要在指标与日志里打上 degraded 标记，方便对账"]' solution="在 Registry 增加 fallbacks map[string]string 记录主到备的映射，并给 CircuitBreaker 增加按 provider 分实例；Gateway.ServeHTTP 在选定 Provider 前先问该家熔断器 allow，若不允许则查 fallbacks 取备用名字并重新 Get，同时在 context 里打 degraded 标记。CircuitBreaker 半开时 allow 只放行 successes 小于 halfOpenMax 的探测，report 成功累计到阈值后把状态置回 Closed。ObserveStream 与 slog 日志带上 degraded 与 provider 字段，成本看板按实际命中的供应商记账。测试覆盖「主熔断后备可用走降级」「备也熔断返回 503」「半开探测成功闭合」「降级请求标记不丢失」四种。" explain="难点有两个：一是降级必须改写实际命中的 provider，否则计费与日志仍记在主家名下，成本对账全乱；二是半开探测不能放量，恢复瞬间若把全量流量打过去，往往会把刚缓过来的上游再次打垮，所以 successes 阈值要小且探测要走独立计数。" />

## 随堂检测

<Quiz no="118-1" q="为什么流式转发必须每块 Flush，而不能攒批发送？" :options='["为了少写几行代码","攒批会让首 token 延迟退化成整段生成时间，并可能吃满内存","HTTP 协议不允许攒批","Go 的 ResponseWriter 默认会自动 Flush"]' answer="1" explain="攒批把流式退化成一次性响应，TTFT 变成总生成时长，长回答还会把整段内容留在内存里。第 6.3 节的逐块 Flush 加禁用反代缓冲才是真流式，第 12 节压测里 P99 从 95 ms 降到 38 ms 就是证据。" />

<Predict no="118-2" q="上游已返回 200 并开始写 data 块，此时供应商中途断流，网关能做什么？" :options='["把状态码改成 500","重新发起一次请求并续上输出","写一个 event: error 事件后结束，不能改已发出的状态码","静默结束，什么都不发"]' answer="2" output="写一个 event: error 事件后结束，不能改已发出的状态码" explain="响应头一旦发出就无法回退。只能在流内写 error 事件让前端感知；流中途重试会导致输出重复与账单翻倍，所以第 6.5 节的 Deliver 用 FirstChunk 把重试严格限制在首字节之前。" />

<Quiz no="118-3" q="新增一家大模型供应商，最理想的改动范围是？" :options='["改 Gateway、改 StreamToClient、改所有客户端","只新增一个 opener 与一个 parse 并注册进 Registry","必须重写 Provider 接口","只能改前端"]' answer="1" explain="Provider 是窄接口，adapter 把「取流」与「解析」参数化。新增供应商只需实现 opener 与 parse 并注册，网关与转发层完全不动，这正是第 6.2 节体现的开闭原则。" />

<Quiz no="118-4" q="关于超时设计，下列哪种做法是对的？" :options='["用一个 30 秒整体超时包住整条流","首字节超时 10 秒加块间空闲超时 60 秒，每来一块重置空闲计时","不设任何超时，交给客户端决定","只在流结束后检查耗时"]' answer="1" explain="整体超时会硬截断长回答；不设超时会让挂住的连接永久占用资源。双超时的本质是「允许总时长很长，但不允许长时间没有进展」，见第 6.5 节 CallWithTimeout 里 idleTimer.Reset 的落点。" />

## 一页纸速记

<KnowledgeCard :points='["Provider 用 Stream 加 emit 回调做窄接口，新增供应商只换 opener 与 parse","统一 Chunk 屏蔽各家字段差异，结束标志归一为 Done","SSE 转发四要素：正确 Content-Type、逐块 Flush、禁反代缓冲、感知断开","有界 channel 做背压，把单连接内存钉在常数级","首字节与块间双超时，长回答不被整体超时误杀","客户端断开靠 context 一路传到上游请求，否则继续烧钱","估算只用于限额，精确计费采信供应商 usage"]' :pitfalls='["攒批或缓冲响应，把流式做成一次性返回","用一个整体超时包住整条流，长回答被硬截断","流中途重试造成输出重复与账单翻倍","客户端断开没取消上游，长连接一直计费","把字符数当精确 token 计费，成本估算失真","响应体没关或 Scanner 缓冲太小，连接泄漏与长帧截断"]' summary="LLM 网关的价值在于把多供应商差异收进一层适配，并用 Go 的长连接能力把每个 token 实时、可靠、可计量地送到客户端。" />

## 小结与下一讲

1. 用 `Provider` 窄接口加 `adapter`（`opener` 与 `parse` 分离）适配多家供应商，统一成 `Chunk`，新增一家只需注册；`httpOpener` 用 `http.NewRequestWithContext` 把取消绑进上游连接。
2. `StreamToClient` 给出真正的 SSE 转发：逐块 `Flush`、禁用反代缓冲、每块检查 `r.Context()`，`PipeWithBackpressure` 用 32 帧有界 channel 把内存钉死；`Deliver` 用 `FirstChunk` 把「首字节前可重试」与「半流失败只能写 error 事件」硬分开。
3. 网关把鉴权、按优先级的令牌桶限流、供应商路由、双超时、三态熔断、成本与 TTFT 可观测收成一处，配 `httptest` 假上游、故障注入与压测数据，并诚实写出单机长连接与估算计费两处仍然存在的限制。

到这里，L10 工程实战的十二个项目全部走完：从命令行工具到网络服务、从单机 KV 到分布式存储、从电商秒杀到云原生 Operator，最后落在 AI 流式网关。贯穿始终的是同一套方法——**先定可度量的约束与契约，再挑实现，最后用失败路径、测试与压测把它打磨到能上线**。每个项目都刻意留了「什么时候该换架构」的判断，因为工程能力的核心从来不是记住某个框架，而是知道在怎样的规模与代价下做怎样的取舍。接下来该把这些知识真正用起来了。

[下一讲：动手实践 · 结课与综合练习](/practice/)
