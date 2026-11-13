---
title: 第 082 讲 · 自研微服务框架：从 0 到 1
description: 用 Options 装配、生命周期、统一错误与内建可观测收成一套最小框架，并对照 kratos 与 go-zero
---

<StageHeader stage="L7 · RPC 与微服务" level="★★★★★" time="60 分钟" goal="把注册、配置、拦截、可观测收成一套框架" prereq="第 081 讲 · Prometheus / Grafana 与 RED 指标" />

# 第 082 讲 · 自研微服务框架：从 0 到 1

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
		registrar(name) // 对接第 075 讲的注册中心
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

框架最该「默认开启」的就是可观测：把日志、trace 注入、指标采集做成一条中间件链挂在 handler 外，业务一行埋点都不用写。下面的 `Observability` 汇总请求数、错误数与累计耗时，正好对应第 081 讲的 RED 三项；`Build` 把三种横切能力按洋葱顺序装好。这正是 kratos 的 middleware 链与 go-zero 内建治理的同构骨架。

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

// Middleware 是统一拦截链签名，与第 074 讲一致。
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

### 3.2 框架边界与 Functional Options 的推导

判断一个框架该收敛什么，标准只有一条：把「每个服务都重复、且与业务无关」的部分收进来，其余一律留给业务。配置装配、超时与错误契约、拦截链、注册发现、可观测属于前者；领域模型、SQL、第三方 SDK 封装属于后者。这条边界不是审美，而是稳定性约束——最常变的代码如果压在最不该动的底层，框架就会长成谁都不敢改的巨石。

Functional Options 为什么优于全局单例，也能逐条推出来。每个 `WithXxx` 只改一个字段，于是：依赖显式传入，读代码就知道这份配置从哪来；`defaultOptions` 兜底，不传任何 Option 也能跑；调用方按需组合，新增能力只需加一个函数；测试时换几个 Option 就能造出完全隔离的实例。全局单例恰好相反——多实例测试互相干扰、配置来源隐晦、能力一多就无从收拾。而「每个 Option 只改一个字段」还顺带保证了**应用顺序无关**，这是可组合的前提：无论先 `WithAddr` 还是先 `WithTimeout`，最终配置都一样。

统一 Error 之所以要支持 `errors.As` 沿链判定，是因为错误会被 `%w` 层层包装，只有沿错误链还原出稳定码，网关才能把它映射成对外状态码而不泄露内部细节。

<SequenceDiagram title="最小框架的启动与优雅停机" caption="装配后先注册再服务，收到信号先反注册、停接新流量、等在途收尾" :actors='["Main","App","Registry","Handler","Signal"]' :steps='[{"from":"Main","to":"App","label":"New 装配 Options","note":"显式依赖","kind":"call"},{"from":"App","to":"Registry","label":"Start 注册","note":"登记实例","kind":"call"},{"from":"App","to":"Handler","label":"装载拦截链","note":"日志指标追踪","kind":"call"},{"from":"Handler","to":"Handler","label":"处理业务","note":"领域逻辑","kind":"self"},{"from":"Handler","to":"App","label":"内建观测上报","note":"RED 与链路","kind":"call"},{"from":"Signal","to":"App","label":"收到 SIGTERM","note":"退出信号","kind":"call"},{"from":"App","to":"Registry","label":"先反注册","note":"停止派发新请求","kind":"call"},{"from":"App","to":"Handler","label":"停接新流量","note":"关闭监听","kind":"call"},{"from":"Handler","to":"App","label":"在途请求收尾","note":"drain 完成","kind":"return"},{"from":"App","to":"Main","label":"释放资源退出","note":"顺序收尾","kind":"return"}]' />

### 3.3 生命周期顺序与替代方案取舍

生命周期的核心是顺序，而且顺序错了不会立刻报错，只会悄悄漏流量。`Start` 必须先把实例登记进注册中心、起心跳，再开始对外服务；`Stop` 必须**先反注册**，让调用方不再把新请求派发过来，然后停止接新流量、等在途请求收尾、最后释放资源。如果先关监听再反注册，摘除生效前的那段窗口里，仍会有请求打到已经不再处理的实例上。到了 Kubernetes 里，这个顺序要靠 preStop 钩子与 `terminationGracePeriodSeconds` 配合，给在途请求留出足够的收尾时间。

| 装配方式 | 显式性 | 可测试性 | 代价 |
| --- | --- | --- | --- |
| Functional Options | 高，依赖逐个传入 | 换 Option 即隔离实例 | 能力多时 Option 数量增长 |
| wire 依赖注入 | 高，编译期生成 | 需重新生成依赖图 | 引入代码生成步骤 |
| 全局单例配置 | 低，来源隐晦 | 多实例互相干扰 | 写起来快，长期最难维护 |
| 配置驱动 | 中，集中在文件 | 依赖配置文件 | 类型安全靠运行时校验 |

| 框架形态 | 装配 | 治理能力 | 主要取舍 |
| --- | --- | --- | --- |
| 自研最小框架 | Functional Options | 拦截链加内建观测 | 可控、可教学、能力少 |
| kratos | Options 加 wire | middleware、registry、balancer | 分层清晰、贴近标准，要自己拼积木 |
| go-zero | 配置驱动加 goctl | 内建熔断限流降载 | 开箱即用、约定强、定制成本高 |
| Service Mesh | 控制面下发 | 治理全在 sidecar | 应用零感知，但多一跳、运维复杂 |

| 可观测怎么给 | 覆盖度 | 侵入性 | 失败边界 |
| --- | --- | --- | --- |
| 内建默认中间件 | 全量覆盖 | 业务零感知 | 需提供逃生舱允许替换或关闭 |
| 业务按需埋点 | 取决于自觉 | 侵入业务代码 | 出事时才发现没有指标与链路 |
| 外部 sidecar 采集 | 传输层覆盖 | 零侵入 | 拿不到应用级语义属性 |

### 3.4 可验证实验：Option 顺序无关与工程检查

「Option 应用顺序无关」不是口头承诺，用一段表驱动测试就能钉死：

```go
package main

import (
	"testing"
	"time"
)

type Options struct {
	Addr    string
	Timeout time.Duration
}

type Option func(*Options)

func WithAddr(a string) Option {
	return func(o *Options) { o.Addr = a }
}

func WithTimeout(d time.Duration) Option {
	return func(o *Options) { o.Timeout = d }
}

func New(opts ...Option) Options {
	o := Options{Addr: ":8080", Timeout: 2 * time.Second}
	for _, apply := range opts {
		apply(&o)
	}
	return o
}

func TestOptionOrderIrrelevant(t *testing.T) {
	a := New(WithAddr(":9090"), WithTimeout(500*time.Millisecond))
	b := New(WithTimeout(500*time.Millisecond), WithAddr(":9090"))
	if a != b {
		t.Fatalf("Option 顺序不应影响结果")
	}
}
```

```bash
go test -v -run TestOptionOrderIrrelevant
```

```text
=== RUN   TestOptionOrderIrrelevant
--- PASS: TestOptionOrderIrrelevant (0.00s)
PASS
```

逐字段解释：两次 `New` 用完全相反的 Option 顺序装配，若每个 Option 只改自己那一个字段，两份 `Options` 应当相等；`PASS` 证明顺序确实无关，这是可组合性的直接证据。把 Options 换成含 slice、map 等不可比较字段的结构体时，需要用 `reflect.DeepEqual` 或逐字段比较，不能直接用 `!=`。

工程检查还差最后一步——确认整份框架能编译、能过静态检查：

```bash
go build ./...
go vet ./...
```

```text
（无输出，退出码 0）
```

逐字段解释：`go build ./...` 编译全部包，成功时默认不打印任何内容；`go vet ./...` 做静态检查，无告警时同样静默。两条命令都以非零退出码表示问题，这正是 CI 里把它们串进流水线的原因——框架的 Options、Error、Middleware 是对外公共契约，编译与 vet 通过才算守住了这层契约没被破坏。

## 4. 工程实践

对照两个主流框架能看清取舍。kratos 走「分层清晰、可插拔」路线：接口定义、传输、中间件、注册发现各自独立，靠 Options 与 wire 组装，贴近 gRPC 与 OTel 标准，适合需要长期演进、多团队协作的项目，代价是要自己拼不少积木。go-zero 走「约定优于配置」路线：goctl 从 api 文件一键生成 handler、logic、路由与熔断限流，单体到微服务的上手成本极低，代价是目录结构与生成代码约束强，深度定制要和框架的约定搏斗。本讲的最小框架则刻意只保留骨架，用来理解「它们都在解决同一组问题」。

<Compare :panels='[{"label":"全局单例配置","code":"var globalCfg = load()\nserve(globalCfg)","note":"写起来快，但多实例测试困难、配置来源隐晦，能力一多就无从收拾"},{"label":"Options 装配","code":"app := New(WithAddr(a), WithTimeout(t))\napp.Run()","note":"依赖显式可组合、带默认值，测试时换几个 Option 就能造出隔离实例"}]' title="框架该怎么装配" />

真正落地时还有几条经验。框架要提供「逃生舱」：默认策略可用，但允许业务替换某个中间件或关掉某项内建观测，否则遇到特殊场景只能去改框架源码。版本上把 Options、Error、Middleware 这几个对外类型当成公共契约，轻易别改签名，内部实现随意重构。错误码、超时、重试预算这些跨服务约定要有全局规范，框架只是把它们强制执行。可观测必须默认打开且带 trace_id 与低基数 label，出问题时才查得动。最后，自研框架适合「能力收敛、团队规模可控」的场景；如果团队需要代码生成、多协议、成熟治理与社区支持，直接选 kratos 或 go-zero 往往更划算——能看懂它们的分层，正是这一讲的目的。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l7-microservice/lesson-082-framework`](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-082-framework)。clone 仓库后进入 `examples/l7-microservice`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

### 4.2 框架的失败矩阵

框架的错误通常不在功能，而在边界与顺序，一旦写错会波及所有接进来的服务：

| 故障 | 触发点 | 现象 | 防线 |
| --- | --- | --- | --- |
| 优雅退出漏流量 | 先停监听再反注册 | 摘除窗口内请求打到已停实例 | 先反注册再停流量 |
| 可观测漏埋 | 按需开启而非默认内建 | 出事时才发现没有指标与链路 | 默认打开 + trace_id + 低基数 label |
| 装配顺序错位 | Start 与注册顺序反 | 已接流量但注册中心查不到 | 生命周期统一调度顺序 |
| 逃生舱缺失 | 默认策略不可替换 | 特殊场景只能改框架源码 | 允许替换中间件或关某项能力 |
| 公共契约易变 | 随意改 Options 签名 | 所有下游服务跟着改 | 对外类型当契约，内部随意重构 |
| 过度抽象 | 没几个服务先造十层接口 | 框架长成谁都不敢改的巨石 | 只收敛重复且无关业务的部分 |

### 4.3 两条真实工程误解

**误解一：功能越多越好，能收进框架的都收进来。** 框架该收敛的只有「每个服务都重复、且与业务无关」的部分。把领域模型、SQL、第三方 SDK 封装塞进来，等于把最常变的代码压在最不该动的底层，结果就是谁都不敢改的巨石。能力少而边界清楚，比功能多却无法演进更有价值。

<Callout type="warn" title="框架的边界比能力更重要" text="别把业务状态机、SQL 或第三方 SDK 封装塞进框架，那会让最常变的代码压在最不该动的底层；能力少而边界清楚，比功能多却谁都不敢改更有价值。" />

**误解二：优雅退出直接关掉监听 socket 就行。** 关掉监听只能挡住新连接，却改不了注册中心里那个仍然健康的地址。摘除生效前的窗口里，上游仍会不断把新请求派发到这个已经不再处理的实例。正确顺序是先反注册、再停止接新流量、最后等在途请求收尾。

<Compare :panels='[{"label":"错误：先停服务","code":"listener.Close()\nderegister()","note":"停服到摘除之间仍有流量打到已不处理的实例"},{"label":"正确：先反注册","code":"deregister()\nstopAccepting()\ndrain()","note":"先让调用方停止派发，再停接新流量并等在途收尾"}]' title="优雅退出的顺序" />

### 4.4 与后续阶段的呼应

本讲收束的四层能力，在 L8 会逐一落到具体基础设施：[/distributed/kubernetes](/distributed/kubernetes) 的探针与优雅退出把 Start/Stop 顺序变成 Pod 生命周期，[/distributed/gitops](/distributed/gitops) 的 Helm 与灰度发布把「可回滚」升级成声明式流程，[/distributed/sre](/distributed/sre) 与 [/distributed/high-availability](/distributed/high-availability) 则把内建可观测与容错纳入稳定性治理。框架选型本身也是架构决策，[/advanced/architecture](/advanced/architecture) 的模块拆分与演进、[/advanced/senior-path](/advanced/senior-path) 的技术选型方法，能解释为什么「自研还是用 kratos/go-zero」没有标准答案。到 L10，[/projects/cli-codegen](/projects/cli-codegen) 的代码生成补上框架没做的那一环，[/projects/micro-seckill](/projects/micro-seckill) 与 [/projects/micro-ecommerce](/projects/micro-ecommerce) 则把这一整套骨架真正放到峰值流量下检验——能拆开、能装回，才是这一讲的目的。

<Quiz q="优雅退出时，Stop 为什么必须先反注册再停止接新流量？" :options='["反注册更省时间","让调用方先停止派发新请求，避免摘除窗口内流量打到已停实例","注册中心强制要求这个顺序","两者顺序无所谓"]' answer="1" explain="关监听只挡新连接，注册中心里地址仍健康；先反注册让上游停止派发，再停流量并等在途收尾，才能避免摘除窗口内的漏流量。" />

<Predict q="按 3.4 的测试，两个 Option 用完全相反的顺序装配，TestOptionOrderIrrelevant 的结果是？" :options='["失败，顺序影响结果","PASS，两份 Options 相等","直接编译报错","结果取决于随机数"]' answer="1" output="--- PASS: TestOptionOrderIrrelevant (0.00s)" explain="每个 Option 只改一个字段，装配顺序无关，两份 Options 相等；PASS 正是可组合性的直接证据。" />

## 动手练一练
<Exercise title="给最小框架补上配置热更新与熔断默认策略" task="把第 077 讲的配置 Store 与第 079 讲的 Breaker 作为两个内建能力接进 App：用 WithConfigStore 装配，并在 middleware 链里默认加一层熔断，超时用 Options.Timeout 统一注入。" :hints='["新增 Option 只改一个字段，App 里存一个配置 Store 指针","熔断中间件放在拦截链外层，失败达到阈值就短路后续 handler","超时通过 context.WithTimeout 从 Options.Timeout 派生并传给 handler"]' solution="要点一：加 WithConfigStore(s) Option，App 持有 Store，业务用 Get 读当前快照，配置变更由第 077 讲的 Push 热更。\n要点二：Build 里追加 breaker 中间件，调用前 Allow、调用后 Record，Open 状态直接返回统一 Error。\n要点三：入口用 context.WithTimeout(ctx, opts.Timeout) 派生 ctx 并逐层传递，保证 deadline 全链路一致。" explain="把配置、熔断、超时收成框架默认能力，业务就再也不会漏掉这几道防线，这正是「内建」相对「按需调用」的价值。" />

## 随堂检测
<Quiz no="082-1" q="自研框架用 Functional Options 装配，相比全局单例配置最大的好处是什么？" :options='["进程启动更快","依赖显式可组合、带默认值、便于多实例与测试隔离","自动生成的代码更少","天然支持所有传输协议"]' answer="1" explain="Option 把每份依赖显式传入并有默认值兜底，换几个 Option 就能造出隔离实例做测试；全局单例在多实例与来源追踪上都很脆弱。" />

<Predict no="082-2" q="代码 2.2 里 Start 之后调用 Stop，注册与反注册的先后顺序是？为什么？" :options='["先停流量再反注册，顺序无所谓","先反注册再停流量，避免摘除窗口内仍有新请求打到已停实例","只反注册不停流量","顺序由注册中心决定，框架管不着"]' answer="1" output="已注册: [order.Greeter]\nbefore: SayHello\nafter : SayHello\n演示：未等到信号，主动走停机流程\n反注册: order.Greeter\n已停止" explain="优雅退出必须先把实例从注册中心摘除，让调用方停止派发新请求，再停止对外服务；否则摘除生效前的窗口里仍会有流量打到已经不再处理的实例。" />

## 一页纸速记
<KnowledgeCard :points='["框架只收敛重复且无关业务的四层：Options 装配、生命周期、横切拦截链、内建可观测","Functional Options 显式可组合带默认值；统一 Error 带稳定码并支持 errors.As 沿链判定","Start 先注册再服务，Stop 先反注册再停流量，顺序错了就有摘除窗口的漏流量","kratos 走分层可插拔与标准生态，go-zero 走代码生成与约定优先，最小框架用来理解二者共同骨架"]' :pitfalls='["把业务状态机、SQL 或第三方 SDK 封装塞进框架，让最常变的代码压在最底层","Options 没有默认值，每起一个服务都要抄一长串配置","可观测按需开启而非默认内建，出事时才发现没有指标与链路"]' summary="一句话记住：框架 = 显式装配 + 有序生命周期 + 统一横切 + 默认可观测，边界守住才不会长成巨石。" />

## 小结与下一讲

- **装配与错误**：Functional Options 让依赖显式、可组合、带默认值；统一 Error 带稳定码并支持沿错误链判定。
- **生命周期与横切**：Start 注册、Stop 先反注册再停流量；拦截链把日志、认证、追踪、指标内建，业务零埋点。
- **取舍**：kratos 可插拔贴近标准、go-zero 代码生成约定优先；自研框架的价值是理解共同骨架，并在能力收敛时恰好够用。

至此 L7 全部讲次结束，服务间的调用、治理与可观测已经被收成一套可拆可装的体系。下一阶段我们把视角从「服务」抬到「系统」，进入分布式与云原生的世界。

[下一讲：CAP / BASE 与一致性模型](/distributed/cap-base)
