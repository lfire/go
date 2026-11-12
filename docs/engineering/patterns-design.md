---
title: 第 058 讲 · 设计模式与 Functional Options / wire
description: 用 Go 的方式做解耦与装配，掌握 Functional Options、工厂与单例、策略与中间件、依赖注入与 wire，避免 Java 式套壳
---

<StageHeader stage="L5 · 工程化与质量" level="★★★★☆" time="45 分钟" goal="用 Go 的方式做解耦与装配" prereq="第 057 讲 · 错误码、错误栈与统一响应" />

# 第 058 讲 · 设计模式与 Functional Options / wire

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

### 四类装配手法为什么存在

Functional Options 是被「长参数列表」逼出来的。当构造函数变成 `New(addr, timeout, maxConn, tls, retry, logger)`，每加一项都要改签名和所有调用点，而 `New(true, false, true)` 这种布尔参数更是完全读不出含义；把可选项做成 `func(*T)` 后，扩展只需加一个 `WithXxx`，旧调用点一行不动，且 `WithTimeout(5*time.Second)` 自带语义。代价也真实：Option 是闭包、能直接改字段，应用顺序相关（后者覆盖前者），还会绕过构造函数里的校验——这正是工程实践里要补 `Validate` 的原因。

中间件用 `func(Handler) Handler` 而不是接口继承，是因为日志、鉴权、超时属于横切逻辑，塞进每个业务函数会重复几十遍；函数类型让每层只写自己那点事。`Chain` 之所以逆序包装，是要让参数列表里靠前的中间件成为最外层：若顺序遍历，最靠前的反而变成最内层，日志就记不到原始请求形态，与预期正好相反。工厂把「按条件选实现」收在一处，避免 `if kind == "mem"` 散落到每个调用点；`sync.Once` 则解决多 goroutine 并发首次初始化的竞态，手写双重检查在 Go 里很容易写错内存序。

`wire` 选择编译期代码生成而不是反射式运行时注入，是顺着 Go 的脾气：它没有运行时 DI 容器的惯例，生成式方案能在编译期就暴露「某个依赖没提供」的错误，且零运行时开销；代价是多一个生成步骤、`wire_gen.go` 必须入库，接口一改还要重跑生成。至于反对 Java 式套壳，理由很具体——Go 的接口是隐式实现、函数是一等公民、组合优于继承，很多模式几行就能表达；每多建一层就多一处要维护的依赖方向，却不增加任何编译期约束。

| 做法 | 加一个可选项 | 顺序/校验 | 主要代价 |
| --- | --- | --- | --- |
| 长参数列表 | 改签名与所有调用点 | 确定，但布尔参数不可读 | 扩展性最差 |
| Builder 链式调用 | 加一个方法 | 需自己写 Build 校验 | 多一层类型，仍是套壳 |
| Functional Options | 加一个 WithXxx | 顺序敏感、易绕过校验 | 需在 New 末尾集中校验 |
| 全局配置对象 | 改全局结构 | 隐式，测试难替换 | 依赖不可见 |
| wire 生成装配 | 改 provider 再生成 | 编译期报错 | 生成步骤与产物入库 |

### 可验证实验：Option 会不会绕过校验

下面这段程序把「Option 直接改字段」的风险变成一行确定输出：

```go
package main

import (
	"fmt"
)

type Server struct {
	MaxConn int
}

type Option func(*Server)

func WithMaxConn(n int) Option {
	return func(s *Server) { s.MaxConn = n }
}

func (s *Server) Validate() error {
	if s.MaxConn <= 0 {
		return fmt.Errorf("MaxConn 必须为正")
	}
	return nil
}

// New 在所有 Option 应用完之后统一校验
func New(opts ...Option) (*Server, error) {
	s := &Server{MaxConn: 100}
	for _, opt := range opts {
		opt(s)
	}
	return s, s.Validate()
}

func main() {
	_, err := New(WithMaxConn(0))
	fmt.Println("校验结果:", err)
}
```

```text
校验结果: MaxConn 必须为正
```

逐字段看：`WithMaxConn(0)` 直接把字段写成 0，若 `New` 末尾不跑 `Validate`，这个非法值会悄悄生效，直到第一次用连接池才炸，故障点离装配很远。把校验放在所有 Option 应用之后，无论传入多少个、顺序如何，非法组合都在同一处被拦下。这也点明了 Functional Options 的隐藏成本：它把「构造函数是唯一合法入口」这个保证打破了，必须靠集中校验补回来；更稳妥的变体是让 Option 只收集配置值，由构造函数统一应用并校验。

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

### 从 sync.Once 的原子双检查到 Option 闭包的逃逸：编译器替这些模式做了什么

第 192 行说「手写双重检查在 Go 里很容易写错内存序」，依据在 `src/sync/once.go`：`Once` 是 `done atomic.Uint32` 加一个 `m Mutex`。`Do` 先 `LoadUint32(&o.done)` 走 fast path（acquire 语义），为 0 才进 `doSlow`：加锁、二次检查、`defer o.done.Store(1)`（release 语义）保证初始化期间的所有写对后续读者可见。若手写 `if instance == nil { instance = newX() }`，既没有 acquire/release——另一个 goroutine 可能看到 `instance != nil` 但对象字段还没初始化完（编译器与 CPU 都可能重排），在 Go 内存模型下又是 data race。fast path 只有一次原子 load、零加锁，所以 `SharedStore()` 读已初始化的单例很便宜，这也是 `sync.Once` 值得用而不是自己写 DCL 的机制理由。

`Functional Options` 则是编译器参与的另一面。`func WithTimeout(d) Option { return func(*Server){...} }` 返回的闭包捕获了 `d`，逃逸分析会把闭包对象连同 `d` 放到堆；`...Option` 是 `[]Option` 切片，元素是函数值（指向 funcval 的指针），循环里 `opt(sv)` 是间接调用、无法内联。所以 Options 不是零成本：每次构造有若干小堆分配加一次间接调用，换来的是「加一个 `WithXxx` 不改签名」的扩展性。`wire` 生成的是直接构造调用链，编译器能内联、零分配——这正是「编译期生成」与「运行时闭包」的代价差。

可复现实验（Go 1.23.8 实测），用编译器的逃逸与内联诊断把这两处机制看出来：

```go
package main

import (
	"fmt"
	"time"
)

type Server struct{ timeout time.Duration }

type Option func(*Server)

func WithTimeout(d time.Duration) Option {
	return func(s *Server) { s.timeout = d }
}

func New(opts ...Option) *Server {
	s := &Server{timeout: 3 * time.Second}
	for _, o := range opts {
		o(s)
	}
	return s
}

func main() {
	_ = New(WithTimeout(5 * time.Second))
	fmt.Println("ok")
}
```

```bash
go build -gcflags=-m main.go
```

```text
./main.go:12:6:  can inline WithTimeout
./main.go:13:9:  can inline WithTimeout.func1
./main.go:16:6:  can inline New
./main.go:25:21: inlining call to WithTimeout
./main.go:13:9:  func literal escapes to heap
./main.go:16:10: opts does not escape
./main.go:17:7:  &Server{...} escapes to heap
./main.go:25:9:  ... argument does not escape
```

逐字段看：`can inline WithTimeout` / `inlining call to WithTimeout` 说明工厂函数本身可被内联；但 `func literal escapes to heap` 证明它返回的闭包对象仍要堆分配；`&Server{...} escapes to heap` 是被构造对象逃逸；`opts does not escape` 说明变参切片本身留在栈上，可它的元素指向的是堆上的闭包；`... argument does not escape` 是调用点那一侧。合起来的结论：编译器能内联 `WithTimeout`，却内联不了循环里 `opt(sv)` 的间接调用，Options 的小堆分配是模式本身的固有成本。

**从机制推出实践结论，并给可证伪的对照**：Options 适合「构造一次」的场景（服务启动、`cmd` 入口装配），不适合放在每次请求的热路径反复 `New(...)`——把 `New(WithTimeout(...))` 放进循环，每请求都会产生闭包分配，`allocs/op` 随 Option 个数线性上升，而直接 `&Server{timeout: d}` 只有一次对象分配。同理，单例只留给无状态、可共享的客户端：把业务依赖塞进 `sync.Once` 管理的全局实例，单测无法替换，多个测试并发改它还会被 `-race` 报出来。这两条都能用 `go test -bench -benchmem` 与 `-gcflags=-m` 复验，而不是「业界推荐」。

**边界与失效条件**：`-gcflags=-m` 的结论依赖 Go 版本与是否开启优化，内联预算变化会让输出行不同；`sync.Once` 的 `Do` 里若初始化函数 panic，`done` 仍会被置 1、后续调用不再重试，且 `Once` 含 `Mutex` 不可复制；`Once` 保证的是「初始化执行一次且写入可见」，不保证「被初始化对象自身线程安全」。机制能证明「装配时序正确、闭包确有分配」，证明不了「装配出的配置合法」——那仍是上一节 `Validate` 的职责。

**版本前提**：本实验在 Go 1.23.8（darwin/arm64）实测；`sync.Once` 的原子 `done` 实现长期稳定，`sync.OnceFunc` / `OnceValue` / `OnceValues` 自 Go 1.21 起提供；`wire` 以 `v0.6.0` 为准，生成代码的可内联性随编译器版本变化。第 045 讲展开 `sync` 原语，第 027 讲展开逃逸分析。

### 把 `doSlow` 的完整源码摊开：那段被注释亲自否掉的 CAS 实现

上一节用文字描述了 `sync.Once` 的「原子双检查」，这一节把 `src/sync/once.go` 的完整实现摆出来——因为真正的机制细节藏在**源码注释里那段被否掉的错误实现**中，它比结论更能说明「为什么必须有 mutex」。以下是 Go 1.23.8 的 `Do` 与 `doSlow` 原文（去掉版权头）：

```text
func (o *Once) Do(f func()) {
	// Note: Here is an incorrect implementation of Do:
	//
	//	if o.done.CompareAndSwap(0, 1) {
	//		f()
	//	}
	//
	// Do guarantees that when it returns, f has finished.
	// This implementation would not implement that guarantee:
	// given two simultaneous calls, the winner of the cas would
	// call f, and the second would return immediately, without
	// waiting for the first's call to f to complete.
	// This is why the slow path falls back to a mutex, and why
	// the o.done.Store must be delayed until after f returns.

	if o.done.Load() == 0 {
		// Outlined slow-path to allow inlining of the fast-path.
		o.doSlow(f)
	}
}

func (o *Once) doSlow(f func()) {
	o.m.Lock()
	defer o.m.Unlock()
	if o.done.Load() == 0 {
		defer o.done.Store(1)
		f()
	}
}
```

逐行推导，每一处都能反证：

**为什么 fast path 是 `Load` 而不是 `CompareAndSwap`。** 注释里那段 `if o.done.CompareAndSwap(0,1) { f() }` 看着更省事：一个原子操作既判定又占位。但 `Do` 的契约（`once.go` 约 45 行「no call to Do returns until the one call to f returns」）要求**任何一次 `Do` 返回时，`f` 已经完成**。CAS 方案里，两个并发调用中赢家把 `done` 置 1 后开始执行 `f`，输家看到 `done == 1` 就立刻返回——它返回时 `f` 可能还没跑完，于是它之后读到的「已初始化」状态是假的。mutex 的作用不是「让 `f` 互斥」，而是**让输家阻塞在锁上直到赢家把 `f` 跑完并释放锁**；`defer o.done.Store(1)` 又刻意把 `done` 置 1 推迟到 `f()` 返回之后，保证「`done == 1`」与「`f` 已完成」严格绑定。这解释了一个反直觉点：即使有原子操作，也替代不了这里的锁——要的是「等待」语义，不只是「互斥」。

**为什么 `doSlow` 要单独拆成一个函数。** `Do` 里那句 `// Outlined slow-path to allow inlining of the fast-path` 是编译器层面的取舍：把含 `Mutex` 的慢路径拆出去，`Do` 函数体就小到能被内联进每个调用点，于是 `SharedStore()` 读已初始化单例时只剩 `o.done.Load() == 0` 一次原子 load 加一个分支，零函数调用。这与本讲前面 `-gcflags=-m` 看内联是同一套机制，只是这里由标准库作者主动写成了便于内联的形状。

**panic 后 `done` 仍会被置 1。** `defer o.done.Store(1)` 用 `defer` 而非普通语句，意味着即便 `f()` panic，置位照样在 unwind 时执行；源码约 48 行也写明「If f panics, Do considers it to have returned; future calls of Do return without calling f」。实测（Go 1.23.8，darwin/arm64）：

```go
package main

import (
	"fmt"
	"sync"
)

var calls int

func main() {
	var o sync.Once
	for i := 1; i <= 3; i++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("第 %d 次 Do: panic 被捕获 = %v\n", i, r)
				}
			}()
			o.Do(func() { calls++; panic("init boom") })
		}()
	}
	fmt.Println("f 实际执行次数 =", calls)
}
```

```text
第 1 次 Do: panic 被捕获 = init boom
f 实际执行次数 = 1
```

逐字段：三次 `o.Do(...)` 只有第一次的 panic 被捕获并打印，`calls == 1` 证明后两次**连 `f` 都没进**——`done` 在第一次 panic 的 defer 里已被置 1，后续 `Do` 走 `Load() == 0` 为假的 fast path 直接返回。这补上了上一节「边界与失效条件」里只提到、没验证的那句「`Do` 里若初始化函数 panic，`done` 仍会被置 1、后续调用不再重试」。可证伪结论：`sync.Once` 保证的是「`f` 至多执行一次」，不是「`f` 成功执行一次」；把「初始化可能失败、失败要重试」的需求交给 `Once` 会踩坑，那种场景要自己维护状态机或改用 `sync.OnceValues`（Go 1.21 起，会在 panic 后保留错误）。这也和本讲误解二呼应：别把业务依赖藏进单例，尤其当初始化会失败时。

**版本前提**：以上为 Go 1.23.8 的 `src/sync/once.go` 原文，`Once` 的 `done atomic.Uint32` + `m Mutex` 结构、`doSlow` 的 defer 置位自 Go 1.0 以来稳定；`sync.OnceFunc / OnceValue / OnceValues` 自 Go 1.21 起提供。第 045 讲展开 `sync` 原语，上一节讲 `doSlow` 的原子双检查，这里补的是**完整源码、被否掉的 CAS 实现与 panic 语义**，不是重复结论。

### wire 的 provider 是一张有向图：拓扑序、缺依赖与成环

前面提到「wire 是编译期代码生成式依赖注入」「生成的是直接构造调用链」，但没展开它到底怎么把一堆 `NewXxx` 拼成 `wire_gen.go`。把 provider 集合看成**有向图**，这一步就不再是黑盒，也能解释为什么缺依赖、循环依赖能在生成期（而非运行时）报错。

建模：每个 provider 函数签名 `func NewRepo(db *DB, c Config) *Repo` 等价于「产出类型 `*Repo` 依赖入参类型 `*DB` 与 `Config`」，即一条从产出节点指向各入参节点的依赖边。`wire.Build(*Service)` 声明目标类型，注入器就是从目标节点出发、沿边把「谁提供这个类型」查出来的过程；生成物 `wire_gen.go` 则是把这张图**拓扑排序后展开成的一串直接构造调用**——先造没有依赖的 `Config`，再 `NewDB(Config)`，再 `NewRepo(*DB, Config)`，最后 `NewService(*Repo)`。这与上一节 `-gcflags=-m` 看到的「生成代码可内联、零分配」是同一件事：图在生成期就确定了顺序，运行时不需要查表、也不需要反射。

本机未安装 wire 二进制，因此下面用一个**自包含的等价模型**演示这张图的解析算法（输出来自这个程序本身，不是 wire 命令的真实输出，避免把它当作工具实测）。它把 provider 记作「产出类型 ← 入参类型集合」，从目标类型做深度优先解析：

```go
package main

import "fmt"

type provider struct {
	out string
	in  []string
}

func resolve(want string, ps []provider) ([]string, error) {
	byOut := map[string]provider{}
	for _, p := range ps {
		byOut[p.out] = p
	}
	visited, order := map[string]bool{}, []string{}
	var visit func(t string, stack []string) error
	visit = func(t string, stack []string) error {
		for _, s := range stack {
			if s == t {
				return fmt.Errorf("cycle: %v -> %s", stack, t)
			}
		}
		if visited[t] {
			return nil
		}
		p, ok := byOut[t]
		if !ok {
			return fmt.Errorf("no provider found for %s", t)
		}
		for _, dep := range p.in {
			if err := visit(dep, append(append([]string{}, stack...), t)); err != nil {
				return err
			}
		}
		visited[t] = true
		order = append(order, fmt.Sprintf("%s(%v)", p.out, p.in))
		return nil
	}
	if err := visit(want, nil); err != nil {
		return nil, err
	}
	return order, nil
}

func main() {
	good := []provider{
		{"Config", nil},
		{"*DB", []string{"Config"}},
		{"*Repo", []string{"*DB", "Config"}},
		{"*Service", []string{"*Repo"}},
	}
	o, err := resolve("*Service", good)
	fmt.Println("正常图生成顺序:", o, "err =", err)

	missing := []provider{{"Config", nil}, {"*DB", []string{"Config"}}, {"*Service", []string{"*Repo"}}}
	_, err = resolve("*Service", missing)
	fmt.Println("缺 provider:", err)

	cyc := []provider{{"A", []string{"B"}}, {"B", []string{"A"}}}
	_, err = resolve("A", cyc)
	fmt.Println("成环:", err)
}
```

```text
正常图生成顺序: [Config([]) *DB([Config]) *Repo([*DB Config]) *Service([*Repo])] err = <nil>
缺 provider: no provider found for *Repo
成环: cycle: [A B] -> A
```

逐字段：`正常图生成顺序` 是一个合法的拓扑序——每个 provider 出现时，它依赖的类型都已在前面产出（`*Repo` 的入参 `*DB`、`Config` 分别由第 2、1 项提供），这正是 `wire_gen.go` 里构造语句的先后次序；`visited` 集合让 `*Repo` 与 `*DB` 共同依赖的 `Config` 只被构造一次，对应「依赖树里共享节点只 new 一个实例」。三种失败情形都由图的形状决定，与业务代码无关：删掉 `*Repo` 的 provider，解析到 `*Service` 的入参时查 `byOut` 不命中 → 缺依赖；A 依赖 B、B 又依赖 A，`visit` 的 `stack` 里再次出现 A → 成环。把 `err` 换成 panic 并在生成期触发，就是「编译期而非运行时报错」的含义：**图在生成阶段就能判定无解，不必等程序跑起来**。

由此推出几条可证伪的实践结论：其一，`wire.Bind(new(Interface), new(*Impl))` 增加的是「接口类型 ↔ 实现类型」的等价边，本质上仍是往这张图里加节点与边，不改变拓扑排序算法；其二，`wire.NewSet` 只是把若干 provider 打包成一个子图，合并时若两个 provider 产出同一类型且没有 `wire.Struct` / `wire.Value` 消歧，图里就出现「一个类型两个来源」的冲突——这和缺依赖是同一类图问题；其三，生成物必须入库、接口一改要重跑生成（本讲 CI 里的 `git diff --exit-code`），机制依据在这里：provider 图的节点是**类型签名**，类型变了图就变，拓扑序随之变，`wire_gen.go` 必然不同。可证伪对照：若 wire 是运行时反射式 DI，缺依赖应表现为「运行到某个请求才 panic」，而实测的等价模型在解析阶段（相当于生成期）就返回了错误——这正是「编译期代码生成」相对「运行时容器」的取舍。

把它接回本讲主题：Functional Options 是「一个构造函数、若干可选参数」，图退化成单节点；`wire` 是「很多构造函数、依赖成树」，才需要拓扑排序。判断「上一个 `New` 还是上 wire」的量化依据因此可以写成：provider 数量 `n` 与依赖边数 `e`，手写装配的复杂度约 O(n + e) 行的构造调用，当 `e` 多到手工排序容易出错、或构造函数签名频繁变动时，让生成器按图重排才划算；项目里 `e` 很小就上 wire，等于用一整套代码生成去解一张三个节点的图，正是本讲点名的 Java 式套壳。

**边界与失效条件**：上面 `resolve` 是教学用的等价模型，只演示「类型匹配 + 拓扑排序 + 环检测」这三件事，不覆盖 wire 真实实现里的 `wire.Struct` 字段注入、`wire.Value` 常量、`cleanup` 函数、接口绑定消歧与 provider 作用域；它的报错字符串是模型自己定义的，不是 wire 命令的输出；真实 wire 的图节点还带包路径与类型身份，同名不同类型不会误匹配。

**版本前提**：本机 Go 1.23.8（darwin/arm64）实测等价模型；`wire` 以本讲 CI 段钉的 `v0.6.0` 为准（本机未安装该二进制，故不引用其命令输出），`sync.OnceFunc / OnceValue / OnceValues` 自 Go 1.21 起。第 035 讲展开包初始化顺序，本讲上一节讲 `sync.Once` 与 Option 逃逸，这两节补的是「`doSlow` 的完整源码与 panic 语义」和「wire provider 图的解析算法」；第 050 讲的 `cmd` 入口装配、第 060 讲的 `go generate` 生成物入库，都是这张图落地后的工程动作。

### 三条最常见的误解

**误解一：设计模式用得越多，架构越专业。** 反例：一个三个字段的 `Server` 建了 Factory、Builder、Provider、Adapter 四层，新增一个字段要动四处，依赖方向反而更模糊，新人找不到真正的实现在哪。Go 的接口隐式实现、函数是一等公民，多数「模式」几行就能表达。判断标准只有两条：有没有让依赖更清楚、有没有让改动更局部；两个都否，就是套壳。

**误解二：单例方便共享，业务依赖也可以放进去。** 反例：把订单服务做成包级单例，单测想换成 fake 时无从下手，只能连真库；多个测试并发改它还会被 `go test -race` 报数据竞争。正确做法：单例只留给无状态、可共享的客户端（连接池、配置读取器），业务依赖一律通过构造函数注入（呼应第 050 讲）。

**误解三：函数类型永远比接口轻，策略一律用 func。** 反例：策略需要携带状态（缓存、计数、配置）或有几个相关方法时，硬用函数类型会逼出全局变量或闭包捕获一长串参数，既难测也难扩展。无状态、单一行为时用函数类型最轻；带状态或成组行为时，换成一两个方法的小接口反而更清楚。

## 4. 工程实践

`wire` 的定位要说清：它是编译期代码生成式依赖注入，把 `NewService(NewStore(...))` 这类手写装配链生成出来，适合依赖树很深、构造函数一大坨的场景；代价是引入生成步骤与 `wire_gen.go`。小项目里一个 `New` 加几个 Option 往往更直接——**先手写，繁琐到影响可读性了再上生成器**。

Option 也不是完全免费：闭包里若直接改字段，就绕过了构造函数里的校验。稳妥做法是在 `New` 末尾统一跑一次 `Validate`，或者让 Option 只收集配置、由构造函数集中应用，这样无论传入多少个 Option，非法组合都在同一处被拦下。

<Compare title="套壳与克制" :panels='[{"label":"写法 A · Java 式套壳","code":"// 反例：层层包装\nAbstractServerFactoryBuilder\n  .withProvider(...)\n  .build()\n// 新增字段要动三四层","note":"目录很多，依赖方向却更模糊，新人找不到真正的实现"},{"label":"写法 B · 一个 New 加 Option","code":"// 正例：一个 New 加若干 Option\nsrv := New(WithAddr(a), WithTimeout(d))\n// 依赖从参数进来，无隐藏全局状态","note":"默认值、可选项、注入点集中在一处，改动局部"}]' />

另一个判断是**全局状态**：单例好用，但它把依赖藏成了包级变量，测试时无法替换。所以单例只适合无状态、可共享的客户端（连接池、配置读取器），业务逻辑仍然应通过构造函数接收依赖。策略与中间件同理，函数类型比定义一个只有一个方法的接口更轻，但当策略需要携带状态时，就该换成小接口。

### 一次真实故障：两个 Option 把配置覆盖成了默认值

现象：某服务上线后超时配置完全没生效，全部走了默认 3 秒，下游一慢就雪崩式超时。排查路径：

1. 打印启动时生效的配置快照，发现 `timeout` 是默认值，而入口明明传了 `WithTimeout(30*time.Second)`；
2. 追装配链，发现 `New` 在两处被调用：一处带超时 Option、另一处只带地址 Option，最终注入到业务的是后者那个实例；
3. 更隐蔽的是，同一处即便只调一次，两个 Option 的应用顺序也会让后写的覆盖先写的，而当时没有任何校验或日志能指出「最终生效的是哪份」；
4. 根因是 Functional Options 的三个特性叠加——顺序敏感、可重复应用、绕过校验，再碰上装配没有集中在入口；
5. 修复：`New` 末尾统一 `Validate` 并打印脱敏后的生效快照，装配收敛到 `cmd` 入口一次完成（呼应第 050 讲），让 Option 只收集配置值、由构造函数集中应用。

替代做法与代价也摆在这里：长参数列表改签名很痛但顺序确定，Builder 多一层类型且仍要自己写校验，全局配置对象最省事却把依赖藏成隐式状态、测试无法替换。Options 换来的是扩展性与可读性，代价就是必须补上「集中装配 + 末尾校验 + 生效快照」这三件套。

### 把生成物与装配检查接进 CI

```yaml
# .github/workflows/assembly.yml（节选）
- uses: actions/setup-go@v5
  with:
    go-version-file: go.mod
- name: wire 生成物一致性
  run: |
    go install github.com/google/wire/cmd/wire@v0.6.0
    go generate ./...
    git diff --exit-code -- '*_gen.go'
- name: 装配用例
  run: go test -run 'TestNew|TestOptions' ./... -count=1
```

版本锁定理由：`wire` 钉在 `v0.6.0`，否则不同版本生成的 `wire_gen.go` 风格不同，`git diff --exit-code` 会在没有任何人改动时误报；`go-version-file: go.mod` 让生成代码与编译用的 Go 版本一致；生成物必须入库，CI 才有「重跑生成再比 diff」的基准——这与第 053 讲 mockgen、第 060 讲 `go generate` 是同一条思路。`TestNew` 一类用例则专门守住「非法 Option 组合必须报错」，把上一节那个实验变成永久回归。

### 与 L10 实战工程结构的呼应

这些装配手法在 L10 按规模逐级登场：`/projects/kv-1-memory` 用几个 Option 装配存储参数就够了，`/projects/micro-ecommerce`、`/projects/micro-seckill` 依赖树深、构造函数一大坨，才轮到 wire 生成装配链；`/projects/cli-codegen` 用函数类型做可插拔的生成策略；第 082 讲自研框架的 Options 装配更是把这些手法收成一套生命周期。再往前，第 050 讲强调「装配只在 `cmd` 入口做一次」，正是本讲那起故障的解药；第 052 讲的表驱动测试与第 053 讲的边界替身，也都依赖「依赖从构造函数进来、不藏在全局单例里」这个前提。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l5-engineering/lesson-058-patterns-design`](https://github.com/lfire/go/tree/main/examples/l5-engineering/lesson-058-patterns-design)。clone 仓库后进入 `examples/l5-engineering`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="补一个 WithMaxConn Option" task="按前面 Option 的写法，补一个 WithMaxConn(n int) Option，让它把 Server 的 maxConn 字段设为 n。" :hints='["返回类型是 Option，即 func(*Server)","用闭包捕获参数 n","在函数体内给 sv.maxConn 赋值"]' solution="func WithMaxConn(n int) Option {\n    return func(sv *Server) {\n        sv.maxConn = n\n    }\n}" explain="Option 把配置延迟到 New 里统一应用，新增一项不会破坏已有调用点。" />

## 随堂检测

<Quiz no="058-1" q="Functional Options 最主要的收益是什么？" :options='["让程序运行更快","在不改构造函数签名的前提下扩展可选配置","自动实现依赖注入","替代接口做抽象"]' answer="1" explain="新增可选字段只需加一个 WithXxx 函数，New 的签名与所有旧调用点都不用动。" />

<Predict no="058-2" q="连续两次调用 SharedStore()，第二次还会执行 once.Do 里的初始化吗？" :options='["会，每次都重新初始化","不会，sync.Once 只执行一次","会 panic","取决于 GC 是否回收"]' answer="1" output="（不会，返回同一个实例）" explain="sync.Once 内部用原子状态保证 Do 的函数在进程生命周期内仅执行一次，适合无状态共享客户端。" />

## 一页纸速记

<KnowledgeCard :points='["Functional Options 用 func(*T) 收敛可选配置，先默认值后覆盖","策略与中间件用函数类型，Chain 逆序包装成洋葱链","工厂按类型生产实现，sync.Once 保证单例只初始化一次","依赖从构造函数进来，wire 只在装配繁琐时才上"]' :pitfalls='["照搬 Factory / Builder / Adapter 多层套壳","把业务依赖藏进包级单例，测试无法替换","策略需要状态却仍硬用函数类型"]' summary="Go 的模式追求把依赖写清楚、把改动收局部，而不是多建几层壳。" />

## 小结与下一讲

- Options 解决可选配置，中间件链解决横切逻辑，二者都基于函数类型。
- 工厂与 `sync.Once` 负责装配；依赖优先构造注入，`wire` 是深依赖树的生成式补充。
- 反面是 Java 式套壳：接口由使用方按需声明，能一个 `New` 解决就别造四层。

[下一讲：性能剖析：pprof / trace / 火焰图](/engineering/profiling)
