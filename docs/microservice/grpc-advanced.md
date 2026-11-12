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

## 4. 工程实践

拦截器是统一治理的最佳落点，但要有节制。认证、日志、指标、trace 注入、panic 恢复适合放进来；带业务语义的校验仍应留在 handler，否则拦截器会变成谁都不敢改的大泥球。顺序上，日志与 recover 放最外层才能覆盖全部失败，认证紧随其后以便尽早短路，指标埋点贴近业务层才能量到真实耗时。

<Compare :panels='[{"label":"返回裸 error","code":"return nil, errors.New(errText)\n// 调用方只能比对字符串","note":"错误没有结构化，无法据此重试、映射网关状态码，跨语言更无从判定"},{"label":"返回 status","code":"return nil, Errorf(Unauthenticated, reason)\n// 调用方用 CodeOf 判定","note":"错误码稳定可判定，网关可映射成 401，且不会把内部细节泄露给客户端"}]' title="跨服务错误该怎么返回" />

还有几条落地经验：metadata 的 key 一律用小写字母加连字符，二进制值必须以 `-bin` 结尾并做 base64；认证 token 别塞进 `context.WithValue` 的普通 key，走 metadata 才能跨进程。错误码要克制，客户端只应对少数可恢复码（如 Unavailable、DeadlineExceeded）做重试，对 InvalidArgument 这类确定性错误重试纯属浪费。deadline 由入口统一设置，内部调用层层派生；同时给客户端与服务端都配一份超时，任何一端失守都不至于让连接永久挂着。最后，拦截器务必无状态、可并发，别在里面放未加锁的共享 map。

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
