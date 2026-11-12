---
title: 第 82 讲 · 自研微服务框架：从 0 到 1
description: 用 Options 装配、生命周期、统一错误与内建可观测收成一套最小框架，并对照 kratos 与 go-zero
---

<StageHeader stage="L7 · RPC 与微服务" level="★★★★★" time="60 分钟" goal="把注册、配置、拦截、可观测收成一套框架" prereq="第 81 讲 Prometheus / Grafana 与 RED 指标" />

# 第 82 讲 · 自研微服务框架：从 0 到 1

> 前面十讲我们各自造过最小 RPC、拦截器、注册表、负载均衡、配置热更新、网关、熔断、追踪与指标。它们现在还是一堆互不相连的零件。这一讲做收束：把这些能力按「装配 → 生命周期 → 横切 → 可观测」四层装进一个最小框架，让业务只写一个 handler、其余由框架兜住。重点不是再造一个 kratos 或 go-zero，而是想清楚框架的边界在哪、能力怎么插拔、以及工业框架在设计取舍上各自放弃了什么。有了这个视角，你既能读懂成熟框架的源码分层，也能在需要时裁出恰好够用的那一套。

## 这一讲你会得到什么
<CheckList id="l82-goals" title="本讲目标" :items='["用 Functional Options 把注册、超时、可观测等能力装配成一份显式依赖","设计 Start 与 Stop 生命周期，讲清注册和反注册的先后顺序","把统一错误、日志、拦截链、指标与 trace 内建成框架默认能力","对照 kratos 与 go-zero 的装配方式、代码生成与治理能力，说出各自取舍"]' />

## 1. 先建立直觉

把框架想成一台相机的机身。镜头（传输协议）、存储卡（注册中心）、测光模块（可观测）都是可换的配件，机身只负责把它们按统一卡口装好、按正确顺序开机与关机。业务代码是摄影师，只管按快门（写 handler），不该去拧螺丝。判断一个框架好不好，就看两件事：**装配是否显式可控**（换个配件要不要改一大片代码），以及**生命周期是否有章法**（启动注册、关停摘除的顺序对不对）。

自研框架最容易犯的错是「过度抽象」：还没写三个服务，就先造出十层接口和一堆 Factory。本讲的取舍很明确——只收敛那些「每个服务都重复、且与业务无关」的部分：配置装配、超时与错误契约、拦截链、注册发现、可观测。领域模型、SQL、第三方 SDK 封装一律留给业务层。边界守住，框架才不会长成谁都不敢改的巨石。

<ConceptDiagram type="architecture-layer" title="最小框架的四层结构" caption="装配层给出 Options，生命周期层管启停，横切层串拦截链，可观测层内建指标与追踪" />

<FlowTimeline :steps='[{"title":"装配 Options","desc":"默认值加若干 WithXxx 组合出一份显式配置","tone":"blue"},{"title":"注册实例","desc":"Start 时向注册中心登记并启动心跳","tone":"blue"},{"title":"装载拦截链","desc":"日志、认证、指标、trace 统一挂到 handler 外","tone":"green"},{"title":"对外服务","desc":"业务 handler 只处理领域逻辑","tone":"green"},{"title":"统一错误","desc":"错误带稳定码并经拦截器转成对外契约","tone":"orange"},{"title":"优雅停机","desc":"先反注册、停接新流量，再释放资源","tone":"orange"}]' direction="horizontal" />

## 2. 语法与规则

### 2.1 Options 装配与统一错误

Functional Options 是 Go 里最贴合「显式依赖、可组合、有默认值」的装配方式：每个 `WithXxx` 只改一个字段，新增能力就加一个函数，调用方按需组合，测试时换几个 Option 就能造出隔离实例。统一错误则把稳定错误码、可展示消息与可被 `errors.As` 判定的底层原因收进一个类型，对外不泄露内部细节。

```go
package main

import (
	"errors"
	"fmt"
	"time"
)

// Error 是框架统一错误：稳定业务码、可展示消息、可包裹的底层错误。
type Error struct {
	Code    int
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("code=%d msg=%s cause=%v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("code=%d msg=%s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

func CodeOf(err error) int {
	var e *Error
	if errors.As(err, &e) { // 沿错误链判定，跨多层包装仍有效
		return e.Code
	}
	return 500
}

// Options 用函数式选项装配，调用顺序无关，新增能力只需加一个 WithXxx。
type Options struct {
	Name     string
	Addr     string
	Timeout  time.Duration
	Registry string
	Tracing  bool
	Metrics  bool
}

type Option func(*Options)

func WithAddr(a string) Option              { return func(o *Options) { o.Addr = a } }
func WithTimeout(d time.Duration) Option    { return func(o *Options) { o.Timeout = d } }
func WithRegistry(r string) Option          { return func(o *Options) { o.Registry = r } }
func WithObservability(t, m bool) Option    { return func(o *Options) { o.Tracing, o.Metrics = t, m } }

func defaultOptions() Options {
	return Options{Name: "app", Addr: ":8080", Timeout: 2 * time.Second, Tracing: true, Metrics: true}
}

type App struct{ opts Options }

func New(opts ...Option) *App {
	o := defaultOptions()
	for _, apply := range opts {
		apply(&o) // 逐个应用；默认值保证不传也能跑
	}
	return &App{opts: o}
}

func main() {
	a := New(WithAddr(":9090"), WithTimeout(500*time.Millisecond),
		WithRegistry("etcd"), WithObservability(true, true))
	fmt.Printf("装配结果: name=%s addr=%s timeout=%v registry=%s tracing=%v metrics=%v\n",
		a.opts.Name, a.opts.Addr, a.opts.Timeout, a.opts.Registry, a.opts.Tracing, a.opts.Metrics)

	err := &Error{Code: 404, Message: "order not found", Cause: fmt.Errorf("db: no rows")}
	fmt.Println("统一错误:", err)
	fmt.Println("错误码:", CodeOf(fmt.Errorf("wrap: %w", err)))
}
```

<OutputBox title="运行结果" output="装配结果: name=app addr=:9090 timeout=500ms registry=etcd tracing=true metrics=true\n统一错误: code=404 msg=order not found cause=db: no rows\n错误码: 404" note="默认值打底、Option 覆盖；错误经 %w 包装后仍能由 errors.As 还原出稳定码。" />

### 2.2 生命周期：Start、Stop 与优雅退出

生命周期的核心是顺序。`Start` 先把实例登记进注册中心、起心跳，再开始对外服务；`Stop` 必须**先反注册**，让调用方不再把新请求发过来，然后停止接新流量、等在途请求收尾，最后释放资源。顺序反了，摘除窗口内仍会有流量打到已经不再处理的实例上。

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Hook 是框架内建的可观测钩子，请求开始与结束各调一次。
type Hook func(ctx context.Context, method string)

type Server struct {
	mu       sync.Mutex
	started  bool
	services []string
	before   []Hook
	after    []Hook
}

func (s *Server) Use(before, after Hook) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.before = append(s.before, before)
	s.after = append(s.after, after)
}

// Start 统一做注册，业务 handler 之前由框架把横切能力装好。
func (s *Server) Start(registrar func(string)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return fmt.Errorf("already started")
	}
	for _, name := range s.services {
		registrar(name) // 对接第 75 讲的注册中心
	}
	s.started = true
	return nil
}

// Stop 先反注册再停止接流量，这是优雅退出的关键顺序。
func (s *Server) Stop(deregister func(string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return
	}
	for _, name := range s.services {
		deregister(name)
	}
	s.started = false
}

func (s *Server) Invoke(ctx context.Context, method string) {
	for _, h := range s.before {
		h(ctx, method) // 日志、指标、trace 注入都在这里
	}
	// 这里是业务 handler 的位置
	for _, h := range s.after {
		h(ctx, method)
	}
}

func main() {
	s := &Server{services: []string{"order.Greeter"}}
	s.Use(
		func(_ context.Context, m string) { fmt.Println("before:", m) },
		func(_ context.Context, m string) { fmt.Println("after :", m) },
	)
	registered := []string{}
	_ = s.Start(func(n string) { registered = append(registered, n) })
	fmt.Println("已注册:", registered)
	s.Invoke(context.Background(), "SayHello")

	// 真实框架用 signal.NotifyContext 阻塞等待 SIGINT/SIGTERM；这里用超时让演示自动结束
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
		fmt.Println("收到退出信号")
	case <-time.After(50 * time.Millisecond):
		fmt.Println("演示：未等到信号，主动走停机流程")
	}
	s.Stop(func(n string) { fmt.Println("反注册:", n) })
	fmt.Println("已停止")
}
```

<LineByLine code="o := defaultOptions()\nfor _, apply := range opts {\n    apply(&o)\n}\nreturn &App{opts: o}" :notes='["先取一份带默认值的配置，保证不传任何 Option 也能启动","遍历调用方传入的每个 Option 函数","依次把配置改到位，组合顺序不影响最终结果","用装配好的配置构造 App，业务随后只依赖这一份显式配置"]' title="Options 装配逐行拆解" />

## 3. 深入一步

### 3.1 把可观测内建进拦截链

框架最该「默认开启」的就是可观测：把日志、trace 注入、指标采集做成一条中间件链挂在 handler 外，业务一行埋点都不用写。下面的 `Observability` 汇总请求数、错误数与累计耗时，正好对应第 81 讲的 RED 三项；`Build` 把三种横切能力按洋葱顺序装好。这正是 kratos 的 middleware 链与 go-zero 内建治理的同构骨架。

```go
package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

// Observability 是框架内建聚合：计数、错误数、累计耗时，业务零感知。
type Observability struct {
	requests atomic.Uint64
	errors   atomic.Uint64
	latency  atomic.Int64 // 累计纳秒
}

func (o *Observability) Report() (uint64, uint64, time.Duration) {
	return o.requests.Load(), o.errors.Load(), time.Duration(o.latency.Load())
}

// Middleware 是统一拦截链签名，与第 74 讲一致。
type Middleware func(ctx context.Context, method string, next func() error) error

func chain(ms []Middleware, final func() error) func() error {
	for i := len(ms) - 1; i >= 0; i-- { // 反向包裹，数组靠前者在外层
		m := ms[i]
		next := final
		final = func() error { return m(context.Background(), "call", next) }
	}
	return final
}

func Build(obs *Observability) []Middleware {
	logging := func(_ context.Context, method string, next func() error) error {
		fmt.Println("log: 进入", method)
		return next()
	}
	tracing := func(_ context.Context, _ string, next func() error) error {
		// 真实框架在这里 Extract traceparent 并创建子 Span
		return next()
	}
	metrics := func(_ context.Context, _ string, next func() error) error {
		start := time.Now()
		err := next()
		obs.latency.Add(int64(time.Since(start)))
		obs.requests.Add(1)
		if err != nil {
			obs.errors.Add(1)
		}
		return err
	}
	return []Middleware{logging, tracing, metrics}
}

func main() {
	obs := &Observability{}
	handler := func() error { time.Sleep(10 * time.Millisecond); return nil }
	call := chain(Build(obs), handler)
	for i := 0; i < 3; i++ {
		_ = call()
	}
	req, errs, lat := obs.Report()
	fmt.Printf("内建观测: 请求=%d 错误=%d 累计耗时=%v\n", req, errs, lat)
}
```

| 维度 | 本讲最小框架 | kratos | go-zero |
| --- | --- | --- | --- |
| 装配方式 | Functional Options | Options 加依赖注入 wire | 配置驱动加 goctl 代码生成 |
| 传输层 | 抽象 Handler | gRPC 与 HTTP 双协议 | HTTP 加 zRPC |
| 治理能力 | 拦截链加内建观测 | middleware 链、registry、balancer | 内建熔断、限流、自适应降载 |
| 代码生成 | 无 | proto 生成桩 | goctl 一键生成 CRUD 与 API |
| 主要取舍 | 可控、可教学、能力少 | 分层清晰、可插拔、贴近标准 | 开箱即用、约定强、定制成本高 |

| 该由框架做 | 不该由框架做 |
| --- | --- |
| 生命周期、注册发现、拦截链 | 业务状态机与领域规则 |
| 统一错误、日志、指标、trace | 领域模型与仓储实现 |
| 超时、重试、熔断的默认策略 | 具体 SQL 与第三方 SDK 封装 |

<Term word="Functional Options" text="每个 Option 只改一项配置的函数式装配，显式、可组合、带默认值，便于测试隔离" />
<Term word="生命周期钩子" text="Start 与 Stop 上挂载注册、反注册、健康检查等动作，由框架统一调度顺序" />
<Term word="可观测内建" text="把日志、指标、追踪作为默认中间件，业务无需手写埋点即可获得 RED 与链路" />

<Callout type="warn" title="框架的边界比能力更重要" text="别把业务状态机、SQL 或第三方 SDK 封装塞进框架，那会让最常变的代码压在最不该动的层里。Options 一定要有能跑的默认值，否则每起一个服务都要抄一长串配置。优雅退出务必先反注册再停流量，并给在途请求留足收尾时间。" />

## 4. 工程实践

对照两个主流框架能看清取舍。kratos 走「分层清晰、可插拔」路线：接口定义、传输、中间件、注册发现各自独立，靠 Options 与 wire 组装，贴近 gRPC 与 OTel 标准，适合需要长期演进、多团队协作的项目，代价是要自己拼不少积木。go-zero 走「约定优于配置」路线：goctl 从 api 文件一键生成 handler、logic、路由与熔断限流，单体到微服务的上手成本极低，代价是目录结构与生成代码约束强，深度定制要和框架的约定搏斗。本讲的最小框架则刻意只保留骨架，用来理解「它们都在解决同一组问题」。

<Compare :panels='[{"label":"全局单例配置","code":"var globalCfg = load()\nserve(globalCfg)","note":"写起来快，但多实例测试困难、配置来源隐晦，能力一多就无从收拾"},{"label":"Options 装配","code":"app := New(WithAddr(a), WithTimeout(t))\napp.Run()","note":"依赖显式可组合、带默认值，测试时换几个 Option 就能造出隔离实例"}]' title="框架该怎么装配" />

真正落地时还有几条经验。框架要提供「逃生舱」：默认策略可用，但允许业务替换某个中间件或关掉某项内建观测，否则遇到特殊场景只能去改框架源码。版本上把 Options、Error、Middleware 这几个对外类型当成公共契约，轻易别改签名，内部实现随意重构。错误码、超时、重试预算这些跨服务约定要有全局规范，框架只是把它们强制执行。可观测必须默认打开且带 trace_id 与低基数 label，出问题时才查得动。最后，自研框架适合「能力收敛、团队规模可控」的场景；如果团队需要代码生成、多协议、成熟治理与社区支持，直接选 kratos 或 go-zero 往往更划算——能看懂它们的分层，正是这一讲的目的。

## 动手练一练
<Exercise title="给最小框架补上配置热更新与熔断默认策略" task="把第 77 讲的配置 Store 与第 79 讲的 Breaker 作为两个内建能力接进 App：用 WithConfigStore 装配，并在 middleware 链里默认加一层熔断，超时用 Options.Timeout 统一注入。" :hints='["新增 Option 只改一个字段，App 里存一个配置 Store 指针","熔断中间件放在拦截链外层，失败达到阈值就短路后续 handler","超时通过 context.WithTimeout 从 Options.Timeout 派生并传给 handler"]' solution="要点一：加 WithConfigStore(s) Option，App 持有 Store，业务用 Get 读当前快照，配置变更由第 77 讲的 Push 热更。\n要点二：Build 里追加 breaker 中间件，调用前 Allow、调用后 Record，Open 状态直接返回统一 Error。\n要点三：入口用 context.WithTimeout(ctx, opts.Timeout) 派生 ctx 并逐层传递，保证 deadline 全链路一致。" explain="把配置、熔断、超时收成框架默认能力，业务就再也不会漏掉这几道防线，这正是「内建」相对「按需调用」的价值。" />

## 随堂检测
<Quiz no="82-1" q="自研框架用 Functional Options 装配，相比全局单例配置最大的好处是什么？" :options='["进程启动更快","依赖显式可组合、带默认值、便于多实例与测试隔离","自动生成的代码更少","天然支持所有传输协议"]' answer="1" explain="Option 把每份依赖显式传入并有默认值兜底，换几个 Option 就能造出隔离实例做测试；全局单例在多实例与来源追踪上都很脆弱。" />

<Predict no="82-2" q="代码 2.2 里 Start 之后调用 Stop，注册与反注册的先后顺序是？为什么？" :options='["先停流量再反注册，顺序无所谓","先反注册再停流量，避免摘除窗口内仍有新请求打到已停实例","只反注册不停流量","顺序由注册中心决定，框架管不着"]' answer="1" output="已注册: [order.Greeter]\nbefore: SayHello\nafter : SayHello\n演示：未等到信号，主动走停机流程\n反注册: order.Greeter\n已停止" explain="优雅退出必须先把实例从注册中心摘除，让调用方停止派发新请求，再停止对外服务；否则摘除生效前的窗口里仍会有流量打到已经不再处理的实例。" />

## 一页纸速记
<KnowledgeCard :points='["框架只收敛重复且无关业务的四层：Options 装配、生命周期、横切拦截链、内建可观测","Functional Options 显式可组合带默认值；统一 Error 带稳定码并支持 errors.As 沿链判定","Start 先注册再服务，Stop 先反注册再停流量，顺序错了就有摘除窗口的漏流量","kratos 走分层可插拔与标准生态，go-zero 走代码生成与约定优先，最小框架用来理解二者共同骨架"]' :pitfalls='["把业务状态机、SQL 或第三方 SDK 封装塞进框架，让最常变的代码压在最底层","Options 没有默认值，每起一个服务都要抄一长串配置","可观测按需开启而非默认内建，出事时才发现没有指标与链路"]' summary="一句话记住：框架 = 显式装配 + 有序生命周期 + 统一横切 + 默认可观测，边界守住才不会长成巨石。" />

## 小结与下一讲

- **装配与错误**：Functional Options 让依赖显式、可组合、带默认值；统一 Error 带稳定码并支持沿错误链判定。
- **生命周期与横切**：Start 注册、Stop 先反注册再停流量；拦截链把日志、认证、追踪、指标内建，业务零埋点。
- **取舍**：kratos 可插拔贴近标准、go-zero 代码生成约定优先；自研框架的价值是理解共同骨架，并在能力收敛时恰好够用。

至此 L7 全部讲次结束，服务间的调用、治理与可观测已经被收成一套可拆可装的体系。下一阶段我们把视角从「服务」抬到「系统」，进入分布式与云原生的世界。

[下一讲：CAP / BASE 与一致性模型](/distributed/)
