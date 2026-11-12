---
title: 第 35 讲 · 程序启动、init 顺序与包初始化
description: 搞清 main 之前发生了什么：依赖图、init 执行顺序、runtime 初始化与 inittrace 启动耗时分析
---

<StageHeader stage="L3 · 核心机制" level="★★★★☆" time="40 分钟" goal="搞清楚 main 之前发生了什么" prereq="第 34 讲 · defer 实现与 open-coded 优化" />

# 第 35 讲 · 程序启动、init 顺序与包初始化

> 一个 Go 程序在 `main` 的第一行执行之前，已经做完了解析参数、初始化调度器、给所有包级变量求值、跑完所有 `init`。为什么某个包一导入就变慢？为什么 init 里读配置是个坏主意？这一讲把这条隐藏的流水线拆开，并用 `GODEBUG=inittrace=1` 把耗时量出来。

## 这一讲你会得到什么

<CheckList id="L35-goals" title="本讲目标" :items='["按依赖图说清包级变量与 init 的执行先后","划出 init 该做与不该做的边界","概览 runtime 从入口到 main.main 的启动步骤","用 inittrace 分析启动耗时与全局变量的代价"]' />

## 1. 先建立直觉

把启动想成开业前的筹备：先搭好场地（runtime 初始化），再按供货关系逐家备货（被依赖的包先初始化），每家店里先摆好货架（包级变量），再按清单做开业检查（init 函数），全部就绪才开门迎客（main.main）。顺序不是「文件从上到下」这么简单，而是由依赖关系决定的。

<ConceptDiagram type="startup" title="从进程入口到 main.main" caption="runtime 搭台、包按依赖图初始化、最后才调用用户入口" />

## 2. 语法与规则

单文件内就能看清包级变量、多个 init 与 main 的先后。

```go
package main

import "fmt"

var a = setA() // 包级变量先于任何 init 求值
var b = setB() // 依赖 a，故排在 a 之后

func setA() int {
	fmt.Println("1) 包级变量 a 初始化")
	return 1
}
func setB() int {
	fmt.Println("2) 包级变量 b 初始化（依赖 a）")
	return a + 10
}

func init() { fmt.Println("3) 第一个 init，此时 b =", b) }
func init() { fmt.Println("4) 第二个 init") }

func main() {
	fmt.Println("5) main.main 开始，a =", a, "b =", b)
}
```

命令：`go run init_order.go`；再加 `GODEBUG=inittrace=1 go run init_order.go`，能看到 `init main @0.0 ms, 0.0 ms clock, ... bytes, ... allocs` 这样一行，直接给出该包 init 的耗时与分配量。

<OutputBox title="init_order.go 的运行结果" output="1) 包级变量 a 初始化\n2) 包级变量 b 初始化（依赖 a）\n3) 第一个 init，此时 b = 11\n4) 第二个 init\n5) main.main 开始，a = 1 b = 11" note="包级变量按依赖求值，全部就位后才依次执行 init，最后才进 main。" />

<LineByLine code="var a = setA()\nvar b = setB()\nfunc init() { /* ... */ }\nfunc main() { /* ... */ }" :notes='["包级变量按依赖关系求值，先于所有 init","b 引用了 a，因此编译器把 b 排在 a 之后","init 在全部变量就位后按出现顺序执行","所有包的 init 完成，运行时才调用 main.main"]' />

## 3. 深入一步

语言规范给的次序有三层：跨包按 import 依赖图做拓扑排序，被依赖的包先完成；同一包内先给包级变量按依赖求值；再按「文件名字典序、同文件内出现顺序」执行该包所有 `init`。`init` 没有参数、没有返回值、不能被引用，一个包可以有任意多个。

<FlowTimeline :steps='[{"title":"rt0_go","desc":"汇编入口，设置 g0 栈与 argc、argv","tone":"gray"},{"title":"schedinit","desc":"初始化 P 数量、mheap、GC 与栈池","tone":"blue"},{"title":"newproc","desc":"创建 main goroutine，指向 runtime.main","tone":"blue"},{"title":"doInit","desc":"按 inittask 依赖图执行所有包级变量与 init","tone":"violet"},{"title":"main.main","desc":"用户入口，此时全局状态已就绪","tone":"green"}]' direction="horizontal" />

runtime 侧的入口是汇编 `rt0_go`，`schedinit` 里把 P 的数量设为 GOMAXPROCS 并初始化堆、GC 与栈池，随后 `newproc` 造出 main goroutine，`mstart` 启动调度，`runtime.main` 再调用 `doInit` 跑 `..inittask` 依赖图，最后转 `main.main`。这条路径的源码精读留到第 94 讲，本讲只需建立「main 之前有一段确定次序」的认知。链接器生成的 `..inittask` 符号可用 `go tool nm ./binary | grep inittask` 查到。

init 最适合做无副作用、无 I/O 的一次性装配：注册编解码器或驱动、填只读查表、做不变量校验。标准库 `image/png` 在 init 里调 `image.RegisterFormat`，数据库驱动用 `sql.Register`；`import _ "pkg"` 空白导入就是为了只触发这个注册。

```go
package main

import (
	"fmt"
	"sort"
)

// 包级注册表：在 init 之前就已分配好
var drivers = map[string]func() string{}

func register(name string, f func() string) { drivers[name] = f }

// init 里只做登记，不做 I/O、不启动后台任务
func init() {
	register("json", func() string { return "JSON 编解码器" })
	register("yaml", func() string { return "YAML 编解码器" })
}

func init() {
	if len(drivers) == 0 { // 启动时做一次不变量校验，失败就早失败
		panic("没有任何驱动被注册")
	}
}

func main() {
	names := make([]string, 0, len(drivers))
	for n := range drivers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Printf("驱动 %-5s → %s\n", n, drivers[n]())
	}
}
```

命令：`go run registry.go`。这种模式的好处是调用方无需记得显式注册，但也正因如此，init 里一旦塞进重活，所有导入它的程序（包括测试）都会被拖慢。

| 阶段 | 做什么 | 顺序依据 |
| --- | --- | --- |
| 跨包 | 初始化每个包 | import 依赖图拓扑序，被依赖者先 |
| 包级变量 | 求值全局 var | 包内依赖分析，被引用者先 |
| init 函数 | 逐包执行 | 多文件按文件名、同文件按出现顺序 |
| main.main | 用户入口 | 全部包 init 完成之后 |

<Term word="inittask" text="编译器为每个包生成的初始化任务描述，含依赖包列表与待执行函数，链接器拼成一张供 doInit 遍历的图。" />
<Term word="空白导入" text="import _ 包名，不引入标识符，唯一目的是触发该包的 init，常用于注册表式的驱动装配。" />

<Callout type="warn" title="易错点" text="别依赖「两个无依赖包的 init 谁先跑」这种隐含顺序，它随文件名与编译器版本变化；init 里不能返回错误，只能 panic 或记日志，也别在 init 里启动常驻 goroutine，否则测试与优雅退出都会被牵连。" />

## 4. 工程实践

包级变量是「藏在启动期的成本」：`var table = make([]int, 1<<20)` 在 main 之前就要完成约 8 MiB 的堆分配，而且常驻。init 里再做一轮填充，启动耗时与内存都被悄悄抬高。

```go
package main

import (
	"fmt"
	"runtime"
)

// 包级变量：在 main 之前完成分配，且常驻堆
var table = make([]int, 1<<20) // 约 8 MiB

func init() {
	// init 里做重活会拖慢每个导入本包的程序，包括测试
	for i := range table {
		table[i] = i
	}
}

func main() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("main 一开始 HeapAlloc 已有 %d MiB\n", m.HeapAlloc>>20)
	fmt.Printf("table 长度 = %d，首元素 = %d\n", len(table), table[0])
}
```

命令：`GODEBUG=inittrace=1 go run heavy_init.go` 观察 `init main` 那一行的 bytes 与 clock；`go build -gcflags=-m heavy_init.go` 看 `make([]int, 1<<20) escapes to heap`；`go build -x heavy_init.go` 打印编译链接全过程。把重活挪进显式的 `Setup()`，让调用方决定何时做、能否返回错误，通常更可控。

<Compare :panels='[{"label":"塞进 init","code":"func init() {\ncfg = loadConfig()\ndb = connect()\n}","note":"拖慢所有导入方，错误只能 panic，测试难隔离"},{"label":"显式初始化","code":"func main() {\ncfg, err := loadConfig()\nif err != nil { log.Fatal(err) }\n}","note":"可返回错误、按需执行、易测试可替换"}]' title="把重活移出 init" />

启动耗时分析的标准动作就是三步：`GODEBUG=inittrace=1` 找最慢的包，`go tool nm` 看初始化任务，再决定哪些全局变量改成惰性构造。

## 动手练一练

<Exercise title="找出最慢的 init" task="在一个导入了多个包的小程序上跑 GODEBUG=inittrace=1，找出 clock 值最高的包，并说明它的 bytes 与 allocs 意味着什么。" :hints='["inittrace 每行格式：包名 @时刻 ms, 耗时 ms clock, 字节, 分配次数","bytes 大说明 init 期分配多","把可疑的重活从 init 移到显式 Setup"]' solution="GODEBUG=inittrace=1 go run app.go\n观察输出中 clock 最大者：其 bytes 与 allocs 反映 init 期的堆分配；\n若某包字节数异常，检查它的包级变量与 init 是否做了填充或解码。" explain="inittrace 把「main 之前的隐藏成本」量化到包级别，是启动优化的第一手依据。" />

## 随堂检测

<Quiz no="35-1" q="两个互不依赖的包 A 和 B 都被 main 导入，它们的 init 先后由什么决定？" :options='["main 里 import 的书写顺序","依赖图拓扑序加文件名字典序，不应依赖隐含先后","按包名长度","随机"]' answer="1" explain="跨包按依赖图拓扑序、同包按文件名字典序与出现顺序；由于无依赖包之间顺序不稳定，不应写依赖它。" />
<Predict no="35-2" q="代码里写 import _ 某包，最可能发生什么？" :options='["编译报错，因为没有用到该包","只触发该包的 init，不引入任何标识符","把该包所有符号导入当前文件","跳过该包的初始化"]' answer="1" output="该包被正常初始化并执行其 init，当前文件不获得任何新标识符" explain="空白导入的唯一作用是触发 init，典型用途是让编解码器或数据库驱动完成自注册。" />

## 一页纸速记

<KnowledgeCard :points='["次序：跨包按 import 依赖图，包内先变量后 init，最后 main.main","runtime 侧 rt0_go → schedinit → newproc → doInit → main.main","init 适合无 I/O 的注册与校验，空白导入专为触发注册","GODEBUG=inittrace=1 量化每个包的 init 耗时与分配"]' :pitfalls='["在 init 里读配置、连库、启动后台 goroutine","依赖无依赖关系包之间不稳定的 init 先后"]' summary="main 之前是一段由依赖图确定的初始化流水线，重活应显式化而非塞进 init。" />

## 小结与下一讲

- 包级变量与 init 的先后由依赖关系决定，不是简单的书写顺序。
- init 只做轻量装配；全局大对象与重活会变成隐藏的启动成本。
- `GODEBUG=inittrace=1` 是定位启动慢包的第一手工具。

初始化跑完之后，机器真正执行的是编译器和链接器产出的指令。[下一讲：编译、链接、SSA 与内联](/core/compiler)
