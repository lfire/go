---
title: 第 74 讲 · gRPC 拦截器、metadata、错误与超时
description: 把横切能力统一收进拦截器，讲透 metadata 透传、status 错误码与 deadline 传播
---

<StageHeader stage="L7 · RPC 与微服务" level="★★★★☆" time="50 分钟" goal="把横切能力统一收进拦截器" prereq="第 73 讲 Protobuf 与 gRPC 四种流" />

# 第 74 讲 · gRPC 拦截器、metadata、错误与超时

> 契约和四种流解决的是「怎么调」，但真实服务里还有一大堆与业务无关、却每个方法都绕不开的事：鉴权、日志、限流、链路 ID、panic 兜底、超时控制。如果把它们散落在每个 handler 里，代码会迅速膨胀且各写各的。gRPC 给出的答案是拦截器（Interceptor）——像 net/http 的中间件一样，把横切逻辑收成一条洋葱链。本讲不只列 API，而是用标准库把拦截器、metadata、status 错误码和 deadline 传播各造一遍，再对照真实 grpc 包的对应能力，让你明白框架替你做了什么、以及哪条链路一旦写断就会出事。

## 这一讲你会得到什么
<CheckList id="l74-goals" title="本讲目标" :items='["用函数链自建 Unary 拦截器，理解洋葱模型的前置与后置执行顺序","用 context 承载 metadata 做逐跳透传，并知道 key 命名与二进制值的约束","用结构化 status 错误码替代字符串比对，实现可跨语言判定的错误","讲清 deadline 如何沿调用链传播、如何被编码成 grpc-timeout 传给下游"]' />

## 1. 先建立直觉

把一次 RPC 想成过安检登机：业务 handler 是登机口，而鉴权、日志、trace-id 注入、panic 兜底是一道道必经的关卡。请求先由外向内穿过每道关卡的前半段，到达业务，再由内向外穿回每道关卡的后半段——这就是「洋葱模型」。洋葱的价值在于，日志能同时记到开始与结束、耗时与错误；认证能在业务前短路；兜底 recover 能把最内层的 panic 兜在最外层。

与 HTTP 中间件不同，gRPC 把「不属于消息本身」的数据单独放进 **metadata**：一组挂在 context 上的键值，随请求头在进程间逐跳传递，trace-id、鉴权 token、灰度标记都走这里，而不会污染 pb 定义好的业务字段。加上由框架自动传播的 deadline，三者合起来构成了微服务横切能力的骨架。

<ConceptDiagram type="middleware-onion" title="拦截器洋葱模型" caption="请求由外向内穿过日志、认证、恢复各层，命中业务后再由内向外返回" />

<FlowTimeline :steps='[{"title":"日志前置","desc":"最外层记录进入时间与 FullMethod","tone":"blue"},{"title":"认证","desc":"校验 token，不合法直接短路返回","tone":"blue"},{"title":"恢复兜底","desc":"recover 捕获内层 panic 转成 Internal 错误","tone":"blue"},{"title":"业务 handler","desc":"执行真正的 SayHello 逻辑","tone":"green"},{"title":"后置日志","desc":"由内向外记录耗时与错误","tone":"orange"},{"title":"回写响应","desc":"结果或 status 错误沿原路返回客户端","tone":"orange"}]' direction="horizontal" />

## 2. 语法与规则

### 2.1 自建一条拦截器链

拦截器本质是一个「接收 handler、返回新 handler」的函数。关键是组装顺序：从最后一个往第一个反向包裹，才能让数组里靠前的拦截器处在外层。下面这版把日志、认证串成洋葱，直接用标准库就能跑。

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"
)

type MethodInfo struct{ FullMethod string }

type UnaryHandler func(ctx context.Context, req any) (any, error)
type UnaryInterceptor func(ctx context.Context, req any, info *MethodInfo, handler UnaryHandler) (any, error)

// chain 把多个拦截器反向串成洋葱：数组里越靠前越在外层，先做前置逻辑，最后才到业务。
func chain(interceptors []UnaryInterceptor, final UnaryHandler) UnaryHandler {
	for i := len(interceptors) - 1; i >= 0; i-- {
		in := interceptors[i]
		next := final
		final = func(ctx context.Context, req any) (any, error) {
			return in(ctx, req, &MethodInfo{FullMethod: "/demo.Greeter/SayHello"}, next)
		}
	}
	return final
}

func loggingInterceptor(ctx context.Context, req any, info *MethodInfo, handler UnaryHandler) (any, error) {
	start := time.Now()
	log.Printf("进入 %s", info.FullMethod)
	resp, err := handler(ctx, req) // 调用链继续向内
	log.Printf("退出 %s 耗时 %v err=%v", info.FullMethod, time.Since(start), err)
	return resp, err
}

type ctxKey string

const tokenKey ctxKey = "token"

func authInterceptor(ctx context.Context, req any, _ *MethodInfo, handler UnaryHandler) (any, error) {
	token, _ := ctx.Value(tokenKey).(string)
	if token == "" {
		return nil, fmt.Errorf("unauthenticated: 缺少 token")
	}
	return handler(ctx, req)
}

func business(_ context.Context, req any) (any, error) {
	name, _ := req.(string)
	return "hello, " + name, nil
}

func main() {
	h := chain([]UnaryInterceptor{loggingInterceptor, authInterceptor}, business)
	ctx := context.WithValue(context.Background(), tokenKey, "abc123")
	out, err := h(ctx, "gopher")
	fmt.Printf("结果: %v 错误: %v\n", out, err)

	// 不带 token 再调一次，认证层应当短路
	_, err2 := h(context.Background(), "gopher")
	fmt.Println("无 token 时:", err2)
}
```

<OutputBox title="运行结果" output="结果: hello, gopher 错误: nil\n无 token 时: unauthenticated: 缺少 token" note="认证层在业务之前短路，业务 handler 根本没被执行；日志层的前后两条记录夹住整段耗时。" />

### 2.2 metadata 与 status 错误码

gRPC 的 metadata 是 `map[string][]string`，客户端用 Outgoing context 挂载、服务端用 Incoming context 提取，网关要继续往下调就得把关心的值再挂回 Outgoing context——漏了这一步就是最常见的「trace-id 到第二跳就断」。错误则用 `status` 表达：一个稳定的错误码加一句消息，调用方按码判定，绝不比对字符串。

```go
package main

import (
	"context"
	"fmt"
)

type mdKey struct{} // 自定义类型作 context key，避免跨包冲突

// Metadata 类比 grpc/metadata.MD：本质是 map[string][]string，挂在 context 上透传。
type Metadata map[string][]string

func NewOutgoingContext(ctx context.Context, md Metadata) context.Context {
	return context.WithValue(ctx, mdKey{}, md)
}
func FromIncomingContext(ctx context.Context) (Metadata, bool) {
	md, ok := ctx.Value(mdKey{}).(Metadata)
	return md, ok
}
func (m Metadata) Get(k string) string {
	if vs := m[k]; len(vs) > 0 {
		return vs[0]
	}
	return ""
}

// Status 类比 grpc/status：把错误码与消息做成可判定的 error。
type Code int

const (
	OK Code = iota
	InvalidArgument
	Unauthenticated
	DeadlineExceeded
	Internal
)

type Status struct {
	Code Code
	Msg  string
}

func (s *Status) Error() string { return fmt.Sprintf("rpc error: code=%d msg=%s", s.Code, s.Msg) }

func Errorf(c Code, format string, a ...any) error {
	return &Status{Code: c, Msg: fmt.Sprintf(format, a...)}
}

// CodeOf 让调用方按码判定；非 status 错误一律归为 Internal，避免泄露内部细节。
func CodeOf(err error) Code {
	if s, ok := err.(*Status); ok {
		return s.Code
	}
	return Internal
}

func main() {
	// 客户端把 trace-id 与鉴权信息塞进 metadata 发出
	ctx := NewOutgoingContext(context.Background(), Metadata{
		"trace-id":      {"t-9527"},
		"authorization": {"Bearer abc"},
	})

	// 服务端逐跳提取；要继续调用下游，还得再挂回 Outgoing context
	md, _ := FromIncomingContext(ctx)
	fmt.Println("透传 trace-id:", md.Get("trace-id"))

	err := Errorf(Unauthenticated, "token 已过期")
	fmt.Printf("错误码=%d 可判定=%v\n", CodeOf(err), CodeOf(err) == Unauthenticated)
}
```

<LineByLine code="func CodeOf(err error) Code {\n    if s, ok := err.(*Status); ok {\n        return s.Code\n    }\n    return Internal\n}" :notes='["对外暴露一个按错误码判定的入口，而不是让调用方读错误消息","用类型断言把 error 还原成我们自建的 Status 指针","命中则返回其中稳定的 Code，供重试、映射 HTTP 状态等逻辑使用","未命中说明是普通 error，统一归为 Internal 防止把栈信息泄露出去","函数以右括号收尾，返回类型始终是 Code"]' title="status 错误码判定逐行拆解" />

## 3. 深入一步

### 3.1 deadline 是怎么一路传下去的

超时最容易被写错的地方，是在中间层用 `context.Background()` 重新起一个 ctx——这一下就把上游的 deadline 链剪断了。正确姿势是永远用传入的 ctx 派生子 ctx：父子的 deadline 取更早的那个，子调用只能缩短、不能延长。gRPC 还会把剩余时间编码成 `grpc-timeout` 这个 metadata 发给下游，让跨进程的对端也遵守同一份预算。

```go
package main

import (
	"context"
	"fmt"
	"time"
)

// grpcTimeout 模拟 gRPC：把 ctx 剩余时间编码成 grpc-timeout metadata 传给下游。
func grpcTimeout(ctx context.Context) string {
	d, ok := ctx.Deadline()
	if !ok {
		return "" // 没有 deadline 就不发这个 header
	}
	ms := time.Until(d).Milliseconds()
	if ms < 0 {
		ms = 0
	}
	return fmt.Sprintf("%dm", ms) // 300m 表示 300 毫秒
}

// downstream 尊重上游 deadline，到点立即收手，不傻等到自己超时。
func downstream(ctx context.Context) error {
	select {
	case <-time.After(2 * time.Second):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func main() {
	// 顶层预算 500 毫秒，沿链路用同一个 ctx 派生
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	fmt.Println("发给下游的 grpc-timeout:", grpcTimeout(ctx))

	child, cancel2 := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel2()
	fmt.Println("子调用 grpc-timeout:", grpcTimeout(child))
	fmt.Println("下游结果:", downstream(child))
}
```

| 横切能力 | 自建模型（本讲） | 真实 gRPC API |
| --- | --- | --- |
| 一元拦截 | UnaryInterceptor 链 | grpc.UnaryServerInterceptor、ChainUnaryInterceptor |
| 流拦截 | 包裹 Recv/Send 的 wrapper | grpc.StreamServerInterceptor |
| 元数据 | Metadata 挂 context | metadata.NewOutgoingContext、FromIncomingContext |
| 错误 | Status 加 Code | status.Error(codes.X, msg)、status.Code(err) |
| 超时 | ctx.Deadline 加 grpc-timeout | 由框架自动编码进 grpc-timeout metadata |

<Term word="metadata" text="挂在 context 上的一组键值，随请求头在进程间透传，承载 trace-id、token 等非业务数据" />
<Term word="status code" text="gRPC 定义的稳定错误码，如 Unauthenticated、DeadlineExceeded，跨语言可判定" />
<Term word="deadline 传播" text="超时预算沿 ctx 与 grpc-timeout 逐跳下传，各层取最早 deadline 而非各自为政" />

<Callout type="warn" title="拦截器里最容易断的两条链" text="一是在中间层用 context.Background 重置 ctx，deadline 与 metadata 会同时断掉；二是网关提取 metadata 后忘记再挂回 Outgoing context，trace-id 到第二跳就消失。拦截器里也别忘了 recover，否则最内层 panic 会直接击穿整个进程。" />

### 3.2 metadata 为什么走 HTTP/2 头，状态码为什么落在 trailer

metadata 承载的是「不属于消息本身」的数据：trace-id、鉴权 token、灰度标记。它必须放在 protobuf 载荷之外，否则要么污染 `.proto` 契约（每个方法都得声明一遍这些字段），要么无法被框架统一处理。HTTP/2 把它们放进 HEADERS 帧，于是增加一种横切数据完全不需要改消息定义。由此推出一组硬约束，违反任意一条都会静默丢数据：

| 约束 | 原因 | 违反后果 |
| --- | --- | --- |
| key 只用小写字母、数字、连字符、下划线 | gRPC 规范对 key 的合法字符有限定 | 含大写或非法字符的 key 被丢弃 |
| 二进制值的 key 以 -bin 结尾 | 值需先 base64 才能安全放进文本头 | 非 ASCII 字节被破坏 |
| 不占用 grpc- 前缀 | grpc-timeout、grpc-status 等由框架保留 | 与框架头冲突、行为不可预期 |
| 网关提取后要再挂回 Outgoing context | metadata 不会自动跨第二跳 | trace-id 到下游就断链 |

`grpc-timeout` 是框架自动写入的保留头，值是「剩余时间」而非绝对时刻，单位有 H（小时）、M（分）、S（秒）、m（毫秒）、u（微秒）、n（纳秒），例如 `300m` 表示 300 毫秒、`1S` 表示 1 秒。用相对剩余量而不是绝对时刻，是因为两端时钟未必同步；每次 RPC 重新编码一次剩余量，对端据此在本地重建 deadline。

状态码也一样，它不是给人看的字符串，而是一组跨语言稳定的整数码，最终随 HTTP/2 的 trailer（尾部头）返回，而不是塞进响应体。常见码与语义：

| gRPC code | 数值 | 语义 | HTTP 映射 | 是否可重试 |
| --- | --- | --- | --- | --- |
| OK | 0 | 成功 | 200 | 不适用 |
| InvalidArgument | 3 | 参数错误，确定性失败 | 400 | 否 |
| DeadlineExceeded | 4 | 超时 | 504 | 谨慎，共享同一预算 |
| NotFound | 5 | 资源不存在 | 404 | 否 |
| Unauthenticated | 16 | 未认证 | 401 | 否 |
| ResourceExhausted | 8 | 被限流 | 429 | 带退避可重试 |
| Unavailable | 14 | 暂时不可用 | 503 | 是 |
| Internal | 13 | 内部错误 | 500 | 否 |

「只对可恢复码重试」这条经验的根据就在表里：`InvalidArgument` 重试一万次参数还是错的，纯属浪费；`Unavailable` 往往对应实例重启或网络抖动，退避后重试才有意义。这也和 `status.Code(err)` 的判定方式对齐——调用方按码分支，绝不比对消息文本。

### 3.3 拦截器链顺序与超时预算的推导

洋葱链的执行序由「反向包裹」决定：数组里靠前的拦截器处在最外层，前置逻辑最先执行、后置逻辑最后执行。由此可以直接排出最佳顺序，而不是凭感觉：

- **recover 放最外层**：任何内层 panic 都要被它兜住，放里面就有漏网之鱼，最内层一 panic 会击穿整个进程。
- **日志放次外层**：既要记到进入时间与 FullMethod，也要记到最终结果与整段耗时，只有外层能夹住完整区间。
- **认证紧随其后**：尽早短路，非法 token 不该付出后续所有层的成本。
- **指标贴近业务层**：量到的才是真实 handler 耗时，不会被外层日志与认证的开销污染。
- **trace 注入在认证之后**：拿到身份与灰度标之后才打得出有意义的 Span 属性。

真实 gRPC 的 `grpc.ChainUnaryInterceptor(a, b, c)` 正是按 a 到 c 由外到内执行，a 的前置最先、后置最后。流拦截器与一元拦截器不同：它包裹的是 `grpc.ServerStream`，必须重写 `RecvMsg` 与 `SendMsg` 才能在流内逐条消息上打点，一元拦截器拿不到这些消息——这也是流式调用的横切逻辑更难写对的原因。

超时预算的推导只有一条铁律：deadline 是绝对时刻，沿调用链只能缩短、不能延长，子 ctx 永远取父子中更早的那个。跨进程时 gRPC 把它编码成 `grpc-timeout` 相对值发出，对端据此重建本地 deadline。于是中间层必须用传入的 ctx 派生，绝不能用 `context.Background()` 重置——一重置，顶层预算、取消信号、metadata 三者同时断。级联超时的一种典型形态也由此而来：下游若被设了比上游更长的 deadline，上游早已超时返回，下游却还在傻算，白占连接池与 goroutine。

<SequenceDiagram title="超时预算沿调用链逐跳传递" caption="每一跳用剩余时间重新编码 grpc-timeout，全部重试共享同一份顶层预算" :actors='["Client","Gateway","Order","Stock"]' :steps='[{"from":"Client","to":"Gateway","label":"deadline 800ms","note":"入口设预算","kind":"call"},{"from":"Gateway","to":"Order","label":"timeout 700m","note":"扣掉网关开销","kind":"call"},{"from":"Order","to":"Stock","label":"timeout 400m","note":"再扣本地耗时","kind":"call"},{"from":"Stock","to":"Stock","label":"到点收手","note":"尊重剩余预算","kind":"self"},{"from":"Stock","to":"Order","label":"返回超时","note":"不傻等","kind":"return"},{"from":"Order","to":"Gateway","label":"向上传播","note":"同一预算","kind":"return"},{"from":"Gateway","to":"Client","label":"快速失败","note":"全链路一致","kind":"return"}]' />

### 3.4 替代方案取舍：横切逻辑、上下文与超时各放在哪

同一个「非业务能力」往往有几处可放，选错位置会直接决定故障边界。三组对照把取舍说清：

| 横切逻辑放哪 | 代表做法 | 优点 | 失败边界 |
| --- | --- | --- | --- |
| 拦截器链 | grpc.UnaryServerInterceptor | 语言内、零外部依赖、类型安全 | 每种语言要重写，改逻辑要重新发版 |
| handler 内手写 | 业务代码直接埋点 | 上手最快 | 逻辑散落、极易漏，无法统一治理 |
| Service Mesh sidecar | Envoy 加 Istio | 语言无关、集中升级治理策略 | 多一跳、运维与排障复杂度上升 |

| 上下文怎么带 | 能否跨进程 | 适用与代价 |
| --- | --- | --- |
| metadata | 是 | trace-id、token 逐跳透传，网关需显式再挂载 |
| context.WithValue 普通 key | 否 | 仅进程内临时值，出进程即丢 |
| 写进 proto 业务字段 | 是 | 污染契约、每个方法都要声明，不推荐 |

| 超时怎么设 | 解决什么 | 单独使用的缺口 |
| --- | --- | --- |
| 仅客户端 deadline | 调用方不被无限挂住 | 服务端不知预算，仍可能算到天荒地老 |
| 仅服务端兜底超时 | 服务端自我保护 | 调用方与服务端预算不一致，易级联超时 |
| 全链路统一预算 | 顶层设定、逐跳派生、共享同一 deadline | 需要每层都守规矩，任何一层重置 ctx 即破功 |

### 3.5 可验证实验：deadline 不可延长与 grpc-timeout 的观测

先用测试把「子 deadline 只能更早」这条规则钉死，避免只停在口头结论：

```go
package main

import (
	"context"
	"testing"
	"time"
)

func TestChildDeadlineNeverExceedsParent(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// 子 ctx 名义上给 300 毫秒，但父只剩 100 毫秒
	child, cancel2 := context.WithTimeout(parent, 300*time.Millisecond)
	defer cancel2()

	pd, _ := parent.Deadline()
	cd, _ := child.Deadline()
	if cd.After(pd) {
		t.Fatalf("子 deadline 不应晚于父 deadline")
	}
}
```

```bash
go test -v -run TestChildDeadlineNeverExceedsParent
```

```text
=== RUN   TestChildDeadlineNeverExceedsParent
--- PASS: TestChildDeadlineNeverExceedsParent (0.00s)
PASS
```

逐字段解释：测试人为给子 ctx 设了更长的 300 毫秒，`cd.After(pd)` 若为 true 就失败；实际 `PASS` 证明 `context.WithTimeout` 在父子中取了更早的 100 毫秒，子的名义值被完全覆盖。`defer cancel` 与 `defer cancel2` 保证定时器被释放，和原文 3.1 的写法一致。

再用 grpcurl 观察 metadata 与状态码落在 HTTP/2 头上：

```bash
# 带自定义 metadata 调用，服务端拦截器可逐跳读取
grpcurl -plaintext -H 'trace-id: t-9527' -H 'authorization: Bearer abc' \
  -d '{"name":"go"}' localhost:50051 demo.v1.Greeter/SayHello

# -v 观察响应头里的 grpc-status 等 trailer
grpcurl -plaintext -v -d '{"name":"go"}' localhost:50051 demo.v1.Greeter/SayHello
```

`-v` 输出里的关键片段：

```text
Response headers received:
content-type: application/grpc

Response trailers received:
grpc-status: 0
```

逐字段解释：`content-type: application/grpc` 说明载荷是 gRPC 帧而非 JSON；`grpc-status: 0` 是成功码，非 0 时还会多一行 `grpc-message` 承载文本描述。状态码落在 trailer 而不是 headers，是因为服务端要等 handler 执行完才知道最终成败——这一位置差异也解释了为什么流式调用的错误往往在最后一帧才拿到。`-H` 传入的 `trace-id` 与 `authorization` 会出现在请求 HEADERS 里，服务端用 `metadata.FromIncomingContext` 提取；若它还要调下游，必须再 `metadata.NewOutgoingContext` 挂回去，否则就是最常见的第二跳断链。

## 4. 工程实践

拦截器是统一治理的最佳落点，但要有节制。认证、日志、指标、trace 注入、panic 恢复适合放进来；带业务语义的校验仍应留在 handler，否则拦截器会变成谁都不敢改的大泥球。顺序上，日志与 recover 放最外层才能覆盖全部失败，认证紧随其后以便尽早短路，指标埋点贴近业务层才能量到真实耗时。

<Compare :panels='[{"label":"返回裸 error","code":"return nil, errors.New(errText)\n// 调用方只能比对字符串","note":"错误没有结构化，无法据此重试、映射网关状态码，跨语言更无从判定"},{"label":"返回 status","code":"return nil, Errorf(Unauthenticated, reason)\n// 调用方用 CodeOf 判定","note":"错误码稳定可判定，网关可映射成 401，且不会把内部细节泄露给客户端"}]' title="跨服务错误该怎么返回" />

还有几条落地经验：metadata 的 key 一律用小写字母加连字符，二进制值必须以 `-bin` 结尾并做 base64；认证 token 别塞进 `context.WithValue` 的普通 key，走 metadata 才能跨进程。错误码要克制，客户端只应对少数可恢复码（如 Unavailable、DeadlineExceeded）做重试，对 InvalidArgument 这类确定性错误重试纯属浪费。deadline 由入口统一设置，内部调用层层派生；同时给客户端与服务端都配一份超时，任何一端失守都不至于让连接永久挂着。最后，拦截器务必无状态、可并发，别在里面放未加锁的共享 map。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l7-microservice/lesson-074-grpc-advanced`](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-074-grpc-advanced)。clone 仓库后进入 `examples/l7-microservice`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

### 4.2 横切能力的失败矩阵

拦截器与 context 一旦写断，症状往往跨好几个服务才显现，值得按「断在哪条链」逐行核对：

| 故障 | 触发点 | 现象 | 防线 |
| --- | --- | --- | --- |
| 级联超时 | 中间层用 context.Background 重置 ctx | 下游不再受顶层预算约束，慢调用挂死 | 永远用传入 ctx 派生 |
| trace-id 断链 | 网关提取 metadata 后未挂回 Outgoing | 第二跳起链路追踪出现缺口 | 显式 NewOutgoingContext 再挂载 |
| 拦截器共享状态竞态 | 拦截器里放未加锁 map 做计数 | -race 报 DATA RACE，统计错乱 | 无状态或加锁或改用 atomic |
| panic 击穿进程 | 内层 handler panic 且最外层无 recover | 整个进程退出而非单一调用失败 | recover 放洋葱最外层 |
| 对确定性错误重试 | 对 InvalidArgument 做重试 | 请求量翻倍、错误率不变 | 只对可恢复码重试 |
| metadata 大写 key 被丢 | 用了 Trace-Id 之类写法 | 逐跳取不到值，排查无门 | key 全小写加连字符 |

### 4.3 两条真实工程误解

**误解一：中间层用 context.Background() 起一个新 ctx 更干净，不影响调用。** 这是把「想要一个干净的 context」和「切断上游约束」混为一谈。Background 没有任何 deadline、取消信号，也不带 metadata；用它派生下游，等于把第 74 讲最看重的三条链一次性剪断，下游可能无限等待，链路 ID 也从这一跳起消失。

<Compare :panels='[{"label":"错误：Background 重置","code":"ctx := context.Background()\ncall(ctx)","note":"deadline、取消信号、metadata 三条链同时断"},{"label":"正确：派生传入 ctx","code":"ctx, cancel := context.WithTimeout(parent, d)\ndefer cancel()\ncall(ctx)","note":"沿用父预算，逐跳只能缩短不能延长"}]' title="中间层不要重置 context" />

**误解二：metadata 提取一次就会自动透传给所有下游。** Incoming 与 Outgoing 是两套独立的 context 数据：网关用 `FromIncomingContext` 读到的是「别人发给我的头」，直接拿这个 ctx 去调下游，出站请求里并不会自动带上它们。结果就是 trace-id 在网关这一跳之后凭空消失，链路追踪拼不出完整调用树。

<Callout type="warn" title="metadata 不会自动跨第二跳" text="网关 FromIncomingContext 提取后，必须显式用 NewOutgoingContext 把需要的 key 重新挂回出站 context，且 key 一律小写、二进制值以 -bin 结尾，否则 trace-id 到下游就断链。" />

### 4.4 与后续阶段的呼应

本讲的 deadline 逐跳传播，在 [/distributed/distributed-tx](/distributed/distributed-tx) 的 Saga 补偿链路里会被放大成跨多个服务的统一预算；[/distributed/high-availability](/distributed/high-availability) 的级联失败推演，把「某层一个 Background 重置」升级成全站风险；[/distributed/sre](/distributed/sre) 的错误预算，则和「只对可恢复码重试」直接对应——把重试浪费在确定性错误上，就是在白烧错误预算。实战里，[/projects/micro-seckill](/projects/micro-seckill) 的下单链路若有一层重置 ctx，秒杀超时会瞬间放大成雪崩；[/projects/micro-ecommerce](/projects/micro-ecommerce) 用拦截器统一注入 trace-id，正是第 80 讲链路追踪的入口。

<Quiz q="grpc-timeout 头的值 300m 表示多长？" :options='["300 纳秒","300 微秒","300 毫秒","300 分钟"]' answer="2" explain="grpc-timeout 的单位里小写 m 是毫秒、大写 M 是分钟，300m 即 300 毫秒。用相对剩余量而非绝对时刻，是因为两端时钟未必同步。" />

<Predict q="父 ctx 的 deadline 是 100 毫秒，用 WithTimeout(parent, 300ms) 派生子 ctx，子 ctx 实际的 deadline 是？" :options='["父子中更早的 100 毫秒","名义上的 300 毫秒","两者相加 400 毫秒","不设 deadline"]' answer="0" output="子取父子中更早的 100 毫秒" explain="context.WithTimeout 在父子中取更早的 deadline，子只能缩短不能延长预算；3.5 的测试正是用 cd.After(pd) 断言这一点。" />

## 动手练一练
<Exercise title="给拦截器链加上 panic 恢复与 trace-id 透传" task="在 2.1 的 chain 里新增一个 recoverInterceptor 放最外层，把业务里的 panic 转成 Internal 错误；再写一个透传拦截器，从 Incoming metadata 取出 trace-id 并挂到下一跳的 Outgoing context。" :hints='["recover 必须写在 defer 里，且要放在调用 handler 之前注册","用命名返回值把 panic 转成 error，避免返回零值","透传时新建或复制一份 Metadata，只把需要的 key 挂回 context"]' solution="要点一：recoverInterceptor 用 defer 加 recover，命中时把命名返回 err 设为 Errorf(Internal, msg)，它应放在数组最外层。\n要点二：透传拦截器先 FromIncomingContext 取 trace-id，再用 NewOutgoingContext 挂回新 ctx 传给 handler。\n要点三：拦截器顺序是日志、recover、认证、透传、业务，越靠外越先做前置逻辑。" explain="把 recover 放最外层才能兜住内层所有 panic；把 metadata 显式再挂回 Outgoing context，才不会让链路 ID 在网关这一跳断掉。" />

## 随堂检测
<Quiz no="74-1" q="客户端设了 500 毫秒 deadline，服务端调用下游时却用 context.Background() 新建 ctx，会怎样？" :options='["下游继承 500 毫秒预算，一切正常","整条 deadline 链从 Background 处断开，下游可能无限等待","deadline 自动翻倍成 1 秒","gRPC 会直接报错并拒绝请求"]' answer="1" explain="用 Background 起新 ctx 等于抛弃父 ctx 的 deadline 与取消信号，下游不再受顶层预算约束，慢调用会一直挂到自身超时甚至更久。" />

<Predict no="74-2" q="代码 3.1 里顶层 500 毫秒、child 300 毫秒，grpcTimeout(child) 与 downstream(child) 的结果大致是？" :options='["约 300m，返回 context deadline exceeded","约 500m，返回 nil","不发 grpc-timeout，返回 nil","约 300m，返回 nil"]' answer="0" output="子调用 grpc-timeout: 300m 附近；下游结果: context deadline exceeded" explain="子 ctx 取父子中更早的 deadline，约 300 毫秒；downstream 的 select 先收到 ctx.Done，立即返回 ctx.Err，不会傻等满 2 秒。" />

## 一页纸速记
<KnowledgeCard :points='["拦截器是接收 handler 返回新 handler 的函数，反向组装成洋葱，前置与后置各执行一次","metadata 是挂在 context 上的 map[string][]string，网关必须显式把值再挂回 Outgoing context 才能逐跳透传","错误用 status 的稳定 Code 表达，按码判定并只对可恢复码重试，绝不比对错误字符串","deadline 永远用传入 ctx 派生，父子取更早者；gRPC 把它编码成 grpc-timeout 传给对端"]' :pitfalls='["中间层用 Background 重置 ctx，剪断 deadline 与 metadata 两条链","拦截器里放未加锁的共享状态，或忘记 recover 让 panic 击穿进程","对 InvalidArgument 这类确定性错误做无意义重试"]' summary="一句话记住：拦截器收横切、metadata 带上下文、status 定错误、ctx 传 deadline，四者共用同一条 context。" />

## 小结与下一讲

- **拦截器**：反向组装成洋葱链，日志与 recover 在外、认证居中、指标贴业务，前置短路与后置记账都在这一层完成。
- **metadata 与 status**：非业务数据走 metadata 且必须逐跳再挂载；错误走稳定错误码，按码判定而非比字符串。
- **deadline**：从入口统一设置，层层用同一 ctx 派生，父子取更早者，并由 grpc-timeout 跨进程传递。

[下一讲：服务注册发现与健康检查](/microservice/discovery)
