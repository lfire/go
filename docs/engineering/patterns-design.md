---
title: 第 58 讲 · 设计模式与 Functional Options / wire
description: 用 Go 的方式做解耦与装配，掌握 Functional Options、工厂与单例、策略与中间件、依赖注入与 wire，避免 Java 式套壳
---

<StageHeader stage="L5 · 工程化与质量" level="★★★★☆" time="45 分钟" goal="用 Go 的方式做解耦与装配" prereq="第 57 讲 · 错误码、错误栈与统一响应" />

# 第 58 讲 · 设计模式与 Functional Options / wire

> 很多从别的语言转来的开发者，会把 Factory、Builder、Provider、Adapter 一整套照搬进 Go，结果三个字段的服务建了六层目录。Go 的接口、函数类型与组合已经足够表达绝大多数「模式」，多余的壳只会增加阅读成本。这一讲只讲四类真正用得上的装配手法，并把判断标准定在：**它有没有让依赖更清楚、让改动更局部**。

## 这一讲你会得到什么
<CheckList id="goal-58" title="本讲目标" :items='["用 Functional Options 收敛可选配置，避免长参数列表","用函数类型实现策略与中间件洋葱链","用工厂、sync.Once 单例与构造注入完成装配，并认清 wire 的位置","识别 Java 式套壳，判断何时一个 New 就够了"]' />

## 1. 先建立直觉

把创建对象想成点咖啡：默认值是一杯标准美式，Options 是「去冰、加一份浓缩、换燕麦奶」这些可选项——你不必为每种组合写一个构造函数。中间件则像包在杯子外面的套：日志、鉴权、超时一层层套上去，请求先向内穿过每一层，响应再按相反顺序出来，这就是洋葱模型。

<ConceptDiagram type="middleware-onion" title="中间件洋葱模型：向内穿透、向外返回" />

判断一个模式值不值，只看两件事：它是否把「谁依赖谁」写得更明白，以及新增一种变化时改动是否只落在一处。两个答案都是否，那它就是套壳。

## 2. 语法与规则

第一块是 Functional Options：把可选配置做成一组 `func(*T)`，构造函数先给默认值再依次应用。

```go
// Functional Options：把可选配置收敛成一组函数
package main

import (
	"fmt"
	"time"
)

type Server struct {
	addr    string
	timeout time.Duration
	maxConn int
}

// Option 是修改 Server 配置的函数类型
type Option func(*Server)

func WithAddr(s string) Option {
	return func(sv *Server) { sv.addr = s }
}

func WithTimeout(d time.Duration) Option {
	return func(sv *Server) { sv.timeout = d }
}

func WithMaxConn(n int) Option {
	return func(sv *Server) { sv.maxConn = n }
}

// New 先给默认值，再依次应用 option
func New(opts ...Option) *Server {
	sv := &Server{addr: ":8080", timeout: 3 * time.Second, maxConn: 100}
	for _, opt := range opts {
		opt(sv)
	}
	return sv
}

func main() {
	sv := New(WithAddr(":9090"), WithTimeout(5*time.Second), WithMaxConn(200))
	fmt.Println(sv.addr, sv.timeout, sv.maxConn)
}
```

<OutputBox title="运行结果" output=":9090 5s 200" note="新增一个可选字段只需加一个 WithXxx，不用碰 New 的签名，也不用改所有调用点。" />

第二块用函数类型做策略与中间件，`Chain` 逆序包装形成洋葱链。

```go
// 策略与中间件：用函数类型做可插拔的横切逻辑
package main

import (
	"fmt"
	"strings"
)

// Handler 是业务函数类型
type Handler func(string) string

// Middleware 把一个 Handler 包装成另一个 Handler
type Middleware func(Handler) Handler

func Logging(next Handler) Handler {
	return func(s string) string {
		fmt.Println("log: 收到", s)
		return next(s)
	}
}

func Trim(next Handler) Handler {
	return func(s string) string {
		return next(strings.TrimSpace(s))
	}
}

// Chain 逆序套上中间件，最靠前的在最外层
func Chain(h Handler, mws ...Middleware) Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func main() {
	business := func(s string) string {
		return "hello " + s
	}
	h := Chain(business, Logging, Trim)
	fmt.Println(h("  ada  "))
}
```

```text
log: 收到 ada
hello ada
```

第三块讲装配：工厂按类型生产实现，`sync.Once` 保证单例只初始化一次，而依赖始终从构造函数进来。

```go
// 工厂、单例与构造注入：装配能力也可以很简单
package main

import (
	"fmt"
	"sync"
)

// Store 是依赖抽象
type Store interface {
	Get() string
}

type memStore struct{ v string }

func (m memStore) Get() string { return m.v }

// 工厂：按类型生产实现
func NewStore(kind string) Store {
	switch kind {
	case "mem":
		return memStore{v: "in-memory"}
	default:
		return memStore{v: "default"}
	}
}

// 单例：sync.Once 保证只初始化一次
var (
	once     sync.Once
	instance Store
)

func SharedStore() Store {
	once.Do(func() {
		instance = NewStore("mem")
	})
	return instance
}

// Service 通过构造函数接收依赖，这是最朴素的依赖注入
type Service struct{ store Store }

func NewService(s Store) *Service { return &Service{store: s} }

func main() {
	svc := NewService(SharedStore())
	fmt.Println(svc.store.Get())
	fmt.Println("单例一致:", SharedStore() == svc.store)
}
```

```text
in-memory
单例一致: true
```

<LineByLine title="逐行看一个 Option" code="func WithTimeout(d time.Duration) Option {\n    return func(sv *Server) {\n        sv.timeout = d\n    }\n}" :notes='["导出函数返回一个 Option，命名统一用 With 前缀","返回值是闭包，捕获了参数 d","在真正应用时才修改目标对象","只改一个字段，职责单一","函数闭合"]' />

## 3. 深入一步

模式与惯用写法要对得上号：

| 意图 | Go 惯用写法 | 何时用 |
| --- | --- | --- |
| 可选配置 | Functional Options | 构造参数多且大多有默认值 |
| 可替换算法 | 函数类型或小接口 | 计价、排序、校验策略 |
| 横切逻辑 | Middleware 函数链 | 日志、鉴权、超时 |
| 只初始化一次 | sync.Once | 全局客户端、连接池 |
| 装配依赖树 | 构造注入，深了用 wire 生成 | 依赖层级多到手写繁琐 |

再看一层：这些手法其实都建立在同一个前提上——依赖显式化。Options 显式列出可选项，策略与中间件显式组合行为，构造函数显式接收依赖。Go 不喜欢隐式魔法，凡是让调用方不看实现就猜不到会发生什么的封装，都值得重新掂量。

<Term word="Functional Options" text="把每个可选配置做成一个返回 func(*T) 的函数，构造函数先设默认值再依次应用，从而在不改签名的前提下扩展配置。" />

<Callout type="warn" title="易错点：Java 式套壳" text="为每三个字段就建一层 Factory / Builder / Provider / Adapter，是 Go 项目里最常见的过度设计。接口应当由使用方按需声明，小到一个方法也完全可以；模式是用来缩短代码，不是用来证明架构存在。" />

## 4. 工程实践

`wire` 的定位要说清：它是编译期代码生成式依赖注入，把 `NewService(NewStore(...))` 这类手写装配链生成出来，适合依赖树很深、构造函数一大坨的场景；代价是引入生成步骤与 `wire_gen.go`。小项目里一个 `New` 加几个 Option 往往更直接——**先手写，繁琐到影响可读性了再上生成器**。

Option 也不是完全免费：闭包里若直接改字段，就绕过了构造函数里的校验。稳妥做法是在 `New` 末尾统一跑一次 `Validate`，或者让 Option 只收集配置、由构造函数集中应用，这样无论传入多少个 Option，非法组合都在同一处被拦下。

<Compare title="套壳与克制" :panels='[{"label":"写法 A · Java 式套壳","code":"// 反例：层层包装\nAbstractServerFactoryBuilder\n  .withProvider(...)\n  .build()\n// 新增字段要动三四层","note":"目录很多，依赖方向却更模糊，新人找不到真正的实现"},{"label":"写法 B · 一个 New 加 Option","code":"// 正例：一个 New 加若干 Option\nsrv := New(WithAddr(a), WithTimeout(d))\n// 依赖从参数进来，无隐藏全局状态","note":"默认值、可选项、注入点集中在一处，改动局部"}]' />

另一个判断是**全局状态**：单例好用，但它把依赖藏成了包级变量，测试时无法替换。所以单例只适合无状态、可共享的客户端（连接池、配置读取器），业务逻辑仍然应通过构造函数接收依赖。策略与中间件同理，函数类型比定义一个只有一个方法的接口更轻，但当策略需要携带状态时，就该换成小接口。

## 动手练一练

<Exercise title="补一个 WithMaxConn Option" task="按前面 Option 的写法，补一个 WithMaxConn(n int) Option，让它把 Server 的 maxConn 字段设为 n。" :hints='["返回类型是 Option，即 func(*Server)","用闭包捕获参数 n","在函数体内给 sv.maxConn 赋值"]' solution="func WithMaxConn(n int) Option {\n    return func(sv *Server) {\n        sv.maxConn = n\n    }\n}" explain="Option 把配置延迟到 New 里统一应用，新增一项不会破坏已有调用点。" />

## 随堂检测

<Quiz no="58-1" q="Functional Options 最主要的收益是什么？" :options='["让程序运行更快","在不改构造函数签名的前提下扩展可选配置","自动实现依赖注入","替代接口做抽象"]' answer="1" explain="新增可选字段只需加一个 WithXxx 函数，New 的签名与所有旧调用点都不用动。" />

<Predict no="58-2" q="连续两次调用 SharedStore()，第二次还会执行 once.Do 里的初始化吗？" :options='["会，每次都重新初始化","不会，sync.Once 只执行一次","会 panic","取决于 GC 是否回收"]' answer="1" output="（不会，返回同一个实例）" explain="sync.Once 内部用原子状态保证 Do 的函数在进程生命周期内仅执行一次，适合无状态共享客户端。" />

## 一页纸速记

<KnowledgeCard :points='["Functional Options 用 func(*T) 收敛可选配置，先默认值后覆盖","策略与中间件用函数类型，Chain 逆序包装成洋葱链","工厂按类型生产实现，sync.Once 保证单例只初始化一次","依赖从构造函数进来，wire 只在装配繁琐时才上"]' :pitfalls='["照搬 Factory / Builder / Adapter 多层套壳","把业务依赖藏进包级单例，测试无法替换","策略需要状态却仍硬用函数类型"]' summary="Go 的模式追求把依赖写清楚、把改动收局部，而不是多建几层壳。" />

## 小结与下一讲

- Options 解决可选配置，中间件链解决横切逻辑，二者都基于函数类型。
- 工厂与 `sync.Once` 负责装配；依赖优先构造注入，`wire` 是深依赖树的生成式补充。
- 反面是 Java 式套壳：接口由使用方按需声明，能一个 `New` 解决就别造四层。

[下一讲：性能剖析：pprof / trace / 火焰图](/engineering/profiling)
