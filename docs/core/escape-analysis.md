---
title: 第 27 讲 · 逃逸分析与编译器决策
description: 看懂 -gcflags=-m 并写出少分配的代码：栈分配与逃逸、常见场景、指针返回与减少堆分配的实践
---

<StageHeader stage="L3 · 核心机制" level="★★★★☆" time="40 分钟" goal="看懂 -gcflags -m 并写出少分配的代码" prereq="第 26 讲 · 值语义深潜：赋值、传参、拷贝成本" />

# 第 27 讲 · 逃逸分析与编译器决策

> 上一讲留了个尾巴：取地址可能让对象「搬到堆」。可变量到底住栈还是住堆，从来不由 `new` 或你写不写 `&` 决定，而是编译器在编译期做的一次推断——**逃逸分析**。读懂它，你才能解释「为什么这段代码每次调用都分配内存」「为什么接口传参比具体类型慢」，并用命令而不是猜测去优化。

## 这一讲你会得到什么

<CheckList id="goal-27" title="本讲目标" :items='["能解释栈分配与堆分配的成本差别","会用 go build -gcflags=-m 读懂逃逸决策","能列举并复现五类常见逃逸场景","掌握预分配、避免装箱等减少堆分配的做法"]' />

## 1. 先建立直觉

把函数栈帧想成随身携带的便签本：调用开始撕一页，返回时整页丢掉，零回收成本。堆则是公共公告板：谁都能贴，但要靠 GC 定期打扫，分配和回收都有代价。编译器像一位管理员，逐条检查「这张便签上的地址，会不会被带出房间」。只要指针的生命周期可能长于函数，就判定**逃逸**，把它改贴到公告板上。

<ConceptDiagram type="escape" title="逃逸分析：指针是否活过函数" caption="指针留在函数内 → 栈；被返回、被闭包捕获或装箱外泄 → 堆" />

注意这是**编译期**的保守推断：编译器只在能证明安全时留在栈上，拿不准就一律搬到堆。

## 2. 语法与规则

先对比「返回值」和「返回指针」这两个只差一个符号的函数。

```go
package main

type User struct {
	ID   int
	Name string
}

//go:noinline
func stackUser() User { // 返回的是值拷贝，不因「返回」而逃逸
	u := User{ID: 1, Name: "a"}
	return u
}

//go:noinline
func heapUser() *User { // 返回局部变量地址，u 必须逃逸
	u := User{ID: 2, Name: "b"}
	return &u
}

func main() {
	a := stackUser()
	b := heapUser()
	// 刻意不把结果传给 fmt.Println：...any 会让参数逃逸，干扰观察
	_ = a
	_ = b
}
```

真正的依据不是运行结果，而是编译器的决策日志。注意 `main` 里刻意没有把返回值交给 `fmt.Println`——它的 `...any` 参数本身会让实参逃逸，那样会污染这份日志：

```bash
# -m 打印逃逸决策，-m=2 更详细；grep 只保留与逃逸相关的行
go build -gcflags=-m=2 main.go 2>&1 | grep -E "escapes|moved to heap|leaking"
# 只想看单个文件，也可以直接用编译器
go tool compile -m main.go
```

<OutputBox title="逃逸分析输出（Go 1.23 实测，措辞随版本变化）" output="./main.go:16:2: u escapes to heap:\n./main.go:16:2: moved to heap: u" />

<LineByLine code="./main.go:16:2: u escapes to heap:\n./main.go:16:2: moved to heap: u\nstackUser 的 u 没有出现，说明它留在栈上" :notes='["escapes to heap 是 -m=2 给出的逃逸原因，定位到第 16 行的 u","moved to heap 是最终决策：这个变量的分配改到堆","报告里没有 stackUser 的 u，说明返回值拷贝不需要堆分配"]' />

把常见触发点放在一起，配上 `-gcflags=-m` 就能逐条复现：

```go
package main

import "fmt"

func makeSlice(n int) []byte {
	return make([]byte, n) // 切片要活着离开函数，底层数组在堆上
}

func main() {
	// 1) 被闭包捕获：x 要在函数返回后仍被访问
	x := 42
	f := func() int { return x }
	fmt.Println(f())

	// 2) 装箱进 interface{}：具体类型信息要随值一起存活
	var a any = 42
	fmt.Println(a)

	// 3) 传给 fmt 的 ...any 可变参数
	n := 7
	fmt.Println("n =", n)

	// 4) 动态长度且被返回的切片
	b := makeSlice(n)
	fmt.Println(len(b))
}
```

## 3. 深入一步

结论要有量化依据。`runtime.MemStats.Mallocs` 记录累计堆分配次数，对比两个函数各跑一千次，差异一目了然。

```go
package main

import (
	"fmt"
	"runtime"
)

//go:noinline
func noAlloc() int {
	x := [64]byte{} // 纯栈上，不逃逸
	return int(x[0])
}

//go:noinline
func withAlloc() *int {
	x := 1
	return &x // 逃逸，每次堆分配
}

func countAllocs(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := 0; i < 1000; i++ {
		f()
	}
	runtime.ReadMemStats(&after)
	return after.Mallocs - before.Mallocs
}

func main() {
	fmt.Println("noAlloc 1000 次 Mallocs:", countAllocs(func() { _ = noAlloc() }))
	fmt.Println("withAlloc 1000 次 Mallocs:", countAllocs(func() { _ = withAlloc() }))
}
```

| 场景 | 为什么判定逃逸 | 可验证依据 / 规避方向 |
| --- | --- | --- |
| 返回局部变量指针 | 函数返回后栈帧失效 | `-m` 报 `moved to heap`；改返回值或由调用方持有 |
| 被闭包捕获 | 闭包可能在函数返回后执行 | 只在闭包内用、或显式传值 |
| 装箱进 `any` / 接口 | 动态类型信息需与值同生命周期 | `leaking param`；热路径用具体类型或泛型 |
| 传给 `fmt` 的 `...any` | 参数被当作接口切片 | 日志热路径改用结构化、预格式化 |
| 动态大小且外泄的切片 | 编译期无法定长，又要返回 | `make` 时给足容量，减少扩容次数 |

<Term word="逃逸（escape）" text="指针的引用超出了创建它的函数作用域，编译器据此把本可栈分配的对象改到堆上，交给 GC 管理。" />

<Callout type="warn" title="-m 的输出不是稳定契约" text="不同 Go 版本措辞不同：escapes to heap、moved to heap、leaking param 指的是同一类决策；分析精度也随版本提升，同一段代码升级编译器后输出可能变化。另外「逃逸到堆」不必然是坏事，先 profile 再优化。" />

还要澄清一个流传很广的误解：**返回指针不比返回值慢，也不比它快，是两件事**。返回值可能触发大对象拷贝（见第 26 讲），返回指针则可能触发一次堆分配与后续 GC。哪个划算取决于对象大小、调用频率与是否真的需要共享，结论必须来自 `-gcflags=-m` 加基准测试，而不是口诀。

## 4. 工程实践

最常见的「隐形分配」是切片反复扩容。用 `testing.AllocsPerRun` 可以在普通 `main` 里直接量化预分配的收益。

```go
package main

import (
	"fmt"
	"testing"
)

//go:noinline
func noPrealloc(n int) []int {
	var s []int // nil slice，append 会多次扩容与堆分配
	for i := 0; i < n; i++ {
		s = append(s, i)
	}
	return s
}

//go:noinline
func prealloc(n int) []int {
	s := make([]int, 0, n) // 一次分配到位
	for i := 0; i < n; i++ {
		s = append(s, i)
	}
	return s
}

func main() {
	const n = 1024
	a := testing.AllocsPerRun(100, func() { _ = noPrealloc(n) })
	b := testing.AllocsPerRun(100, func() { _ = prealloc(n) })
	fmt.Printf("无预分配每次约 %.0f 次分配，预分配约 %.0f 次\n", a, b)
}
```

<Compare :panels='[{"label":"反复扩容","code":"var s []int; s = append(s, i)","note":"容量不足时反复分配新数组并拷贝旧元素"},{"label":"一次给足","code":"s := make([]int, 0, n)","note":"单次分配，append 不再触发扩容"}]' title="切片构建：扩容 vs 预分配" />

落地顺序建议是固定的三步：先用 `go build -gcflags=-m=2` 定位哪些变量在逃逸，再用 `go test -bench=. -benchmem` 看 `allocs/op` 有没有真的下降，最后才改代码。常见手段包括：`make` 给足容量、热路径避免 `fmt` 与 `...any` 装箱、能用具体类型或泛型就别用 `any`、把只需局部使用的指针关在函数内。对象复用还可以用 `sync.Pool`，但它和 GC 的关系要到阶段四才展开，别在这里急着套用。

## 动手练一练

<Exercise title="找出逃逸点" task="写一个函数，把局部整型变量的地址存进 []*int 并返回该切片；用 go build -gcflags=-m 观察输出，指出哪一行报告了 escapes to heap 并解释原因。" :hints='["把地址放进会被返回的切片，等于让指针离开函数","-m 会同时打印内联与逃逸两类信息"]' solution="func collect() []*int {\n    x := 1\n    return []*int{&x}\n}" explain="x 的地址通过返回的切片外泄，编译器必须把 x 分配到堆；具体措辞以本机 -m 输出为准。" />

## 随堂检测

<Quiz no="27-1" q="func f() *int { x := 1; return &x } 里的 x 会分配到哪里？" :options='["一定在栈上","一定在堆上","由逃逸分析决定，这里会搬到堆","取决于调用方怎么用"]' answer="2" explain="x 的地址被返回，函数返回后栈帧失效，编译器必须让 x 逃逸到堆；用 go build -gcflags=-m 可看到 moved to heap。" />

<Predict no="27-2" q="给局部变量取地址并传给函数，一定会导致堆分配吗？" :options='["一定会","不一定，编译器看指针是否逃出函数","只有用 new 才会","只有全局变量才会"]' answer="1" output="不一定：若指针没有离开当前函数，仍可留在栈上" explain="逃逸分析看的是「指针生命周期是否超出函数」，不是「有没有取地址」。仅在函数内使用的指针会留在栈上。" />

<FillBlank no="27-3" tpl="查看逃逸分析的常用命令是 go build -gcflags=___ main.go；复用临时对象可以用 ___ 包里的 Pool。" :answers='["-m","sync"]' hint="填编译选项与标准库包名" explain="-m 打印优化与逃逸决策；sync.Pool 用于复用临时对象，它与 GC 的关系在阶段四展开。" />

## 一页纸速记

<KnowledgeCard :points='["变量住栈还是堆由编译期逃逸分析决定，与是否写 new 无关","常见逃逸：返回指针、闭包捕获、装箱进 any、传给 ...any、动态大小且外泄","go build -gcflags=-m=2 与 go test -benchmem 是定位与验证的两件工具","减少堆分配：预分配容量、避免热路径装箱、能把指针关在函数内就别外泄"]' :pitfalls='["以为返回指针一定更快，忽略堆分配与 GC 成本","把 -m 的输出措辞当成跨版本固定的契约","未经 profile 就盲目消灭所有逃逸"]' summary="逃逸分析是编译器的保守推断：指针活得比函数长，就搬到堆。" />

## 小结与下一讲

- 栈分配随函数返回自动回收，堆分配需要 GC；由编译期逃逸分析决定去向，不是 `new` 决定。
- `-gcflags=-m=2` 打印决策，措辞随版本变化；`AllocsPerRun` 与 `benchmem` 用来量化。
- 优化闭环是「定位 → 量化 → 改代码 → 复测」，预分配和少装箱是收益最稳的两种手段。

既然切片扩容既触发堆分配、又改变 `cap`，那它内部的三元组到底怎么排、扩容策略在不同 Go 版本又差多少？下一讲拆开切片看。

[下一讲：切片底层：三元组、扩容与共享陷阱](/core/slice-internals)
