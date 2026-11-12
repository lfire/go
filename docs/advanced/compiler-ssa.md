---
title: 第 98 讲 · 编译器源码：SSA、逃逸与内联
description: 走一遍前端 IR、SSA 优化 pass、内联预算与逃逸分析实现，并用工具亲眼验证优化器做了什么
---

<StageHeader stage="L9 · 资深进阶与源码" level="★★★★★" time="55 分钟" goal="理解优化器替你做了什么" prereq="第 36 讲 · 编译、链接、SSA 与内联 / 第 27 讲 · 逃逸分析" />

# 第 98 讲 · 编译器源码：SSA、逃逸与内联

> 第 27 讲和第 36 讲分别讲过逃逸现象与编译流程，但「编译器到底凭什么决定一个变量上不上堆」「为什么某个函数没被内联」这类问题，答案在 cmd/compile 的源码里。本讲把前端 IR、SSA 构造、优化 pass、内联预算与逃逸分析串成一条链，并给出三个可复现的观察手段：`-gcflags=-m`、`GOSSAFUNC` 与 `go tool compile -S`。源码位置以 Go 1.23.8 为准。

## 这一讲你会得到什么
<CheckList id="L9-98" title="本讲目标" :items='["说出从 AST 到 SSA 再到机器码的编译链", "用 -gcflags=-m 验证逃逸与内联决策", "用 GOSSAFUNC 与 compile -S 看懂一条优化 pass"]' />

## 1. 先建立直觉
把编译器想成一家加工厂：源码先被切成语法树（前端），类型检查后整理成中间表示 IR，再重排成 SSA，也就是「每个变量只被赋值一次」的形式。这种形式让「这个值其实恒等于 3」「这个下标永远不越界」变得一眼可判，于是优化 pass 能放心地删代码、去边界检查、把小函数摊平。最后才挑指令、分寄存器、产出机器码。优化器替你做的，正是这些不需要你手写却决定性能的决定。
<ConceptDiagram type="ssa" title="从源码到机器码的编译链" caption="SSA 让每条优化都有清晰可验证的形式" />

## 2. 语法与规则
先定位目录与关键常量，再用工具验证。以下均来自 Go 1.23.8 的 GOROOT。

- `src/cmd/compile/internal/`：前端在 `noder`、`types2`，IR 在 `ir`，SSA 构造在 `ssagen`，优化 pass 在 `ssa`，逃逸分析在 `escape`，内联在 `inline`。
- `src/cmd/compile/internal/inline/inl.go:48`：`inlineMaxBudget = 80`，默认内联代价预算；`81` 行 `inlineHotMaxBudget = 2000`，PGO 热点可放宽。
- `src/cmd/compile/internal/ssa/compile.go`：pass 列表与 `GOSSAFUNC` 输出 `ssa.html` 的逻辑。
- `src/cmd/compile/internal/ssa/prove.go`：边界检查消除（BCE）等基于 SSA 的证明。

三段程序分别用于观察逃逸、内联与边界检查消除带来的差异。

```go
package main

import "fmt"

// 观察一：两个局部变量分别留在栈上或逃逸到堆，用 -gcflags=-m 验证。
func noEscape() int {
	x := 42
	return x
}

func doesEscape() *int {
	y := 42
	return &y // 指针被返回，y 必须逃逸到堆
}

func main() {
	fmt.Println(noEscape())
	fmt.Println(*doesEscape())
}
```

```go
package main

import "fmt"

// 观察二：小函数满足内联预算会被摊平；noinline 指令可强制对照。
func add(a, b int) int { return a + b }

//go:noinline
func noInline(a, b int) int { return a + b }

func main() {
	fmt.Println(add(1, 2))
	fmt.Println(noInline(3, 4))
}
```

```go
package main

import (
	"fmt"
	"time"
)

// 观察三：SSA 阶段做边界检查消除。sum1 的 i 由 range 约束，
// 可证明不越界；sum2 每次要额外判断 j 的范围。
func sum1(s []int) int {
	total := 0
	for i := range s {
		total += s[i]
	}
	return total
}

func sum2(s []int, idx []int) int {
	total := 0
	for _, j := range idx {
		if j >= 0 && j < len(s) {
			total += s[j]
		}
	}
	return total
}

func bench(f func()) time.Duration {
	start := time.Now()
	for i := 0; i < 2000; i++ {
		f()
	}
	return time.Since(start)
}

func main() {
	s := make([]int, 1024)
	for i := range s {
		s[i] = i
	}
	idx := make([]int, 1024)
	for i := range idx {
		idx[i] = i % 1024
	}
	fmt.Println("sum1 =", sum1(s))
	fmt.Println("sum2 =", sum2(s, idx))
	fmt.Println("sum1 耗时 =", bench(func() { _ = sum1(s) }))
	fmt.Println("sum2 耗时 =", bench(func() { _ = sum2(s, idx) }))
}
```

<OutputBox title="go build -gcflags=-m 的输出片段" output="./main.go:7:6: can inline noEscape\n./main.go:11:6: can inline doesEscape\n./main.go:13:9: &y escapes to heap\n./main.go:21:6: can inline add\n./main.go:24:6: cannot inline noInline: marked go:noinline" note="escapes to heap 说明 y 被分配到堆；can inline 说明函数代价没超过预算 80。实际行号以你的文件为准。" />

<LineByLine title="编译链的四个关键阶段" code="parseFiles 生成 AST\n类型检查得到 IR\nbuildssa 构造 SSA\nopt pass 后生成机器码" :notes='["前端把源码解析成语法树，完成 import 与作用域处理", "类型检查与改写后得到统一的中间表示 IR", "buildssa 把 IR 转成静态单赋值形式，每个值只有一处定义", "一系列 pass 化简后交给 ssagen 选指令、分配寄存器，产出目标码"]' />

## 3. 深入一步
SSA 的价值在于「形式化」：一旦每个值只有唯一定义，常量传播、死代码消除、公共子表达式合并都变成对图的局部改写，正确性容易验证。内联发生在 SSA 之前，按代价预算而非源码行数判断：`inlineMaxBudget = 80`，含调用、闭包、可变参数等额外代价；Go 1.21 起 PGO 会把热点函数的预算抬到 2000。逃逸分析同样在编译期跑，它沿指针流建图，判断某个局部变量是否被外部引用；结论直接决定用栈还是 `newobject`。

<Term word="SSA" text="Static Single Assignment，静态单赋值：每个变量只被赋值一次、每个值只有一处定义，编译器据此做常量传播、消除冗余与证明性质的优化。" />

| 机制 | 源码位置（Go 1.23.8） | 作用 | 验证命令 |
| --- | --- | --- | --- |
| 内联 | `inline/inl.go` 预算 80 | 消除调用开销、暴露更多优化 | `go build -gcflags=-m` |
| 逃逸分析 | `escape/escape.go` | 决定变量住栈还是堆 | `go build -gcflags=-m=2` |
| 常量折叠 / 死代码消除 | `ssa/` 各 pass | 化简 SSA 图 | `GOSSAFUNC=main go build` |
| 边界检查消除 | `ssa/prove.go` | 去掉可证明的越界检查 | `go tool compile -S` |
| 指令选择与寄存器分配 | `ssagen/` | 生成具体机器码 | `go tool objdump` |

<Callout type="warn" title="别把 80 当成函数行数" text="内联预算是代价节点数，不是代码行数；含闭包、defer、可变参数会额外加价。`-gcflags=-m` 的输出格式随版本调整，判断是否内联要看 can inline 与 inlining call，不要硬套某一条固定文案。" />

## 4. 工程实践
写热路径代码时，与其猜编译器有没有优化，不如让编译器自己交代。`GOSSAFUNC=main go build` 会在当前目录生成 `ssa.html`，逐 pass 展示 SSA 图如何被化简；`go tool compile -S` 打印汇编，可数出循环里还剩几条边界检查。把这些命令接进 CI，能在性能回归合并之前就发现「某个改动意外让变量逃逸」这类问题。

```bash
GOSSAFUNC=main go build -o /dev/null .
go tool compile -S main.go
```

<Compare title="返回指针还是返回值" :panels='[{"label":"写法 A：返回指针","code":"func f() *User {\n    u := new(User)\n    return u\n}","note":"对象逃逸到堆，每次调用一次分配，给 GC 增加压力"},{"label":"写法 B：返回值","code":"func f() User {\n    return User{}\n}","note":"小结构体留在栈或寄存器里，零堆分配，用 -gcflags=-m 可验证"}]' />

## 动手练一练
<Exercise title="找出逃逸点" task="用 -gcflags=-m 跑观察一，找出 escapes to heap 那一行；再把它改成返回值版本，确认该行消失。" :hints='["-m=2 会打印更详细的决策链", "关注 moved to heap 与 escapes to heap 两种措辞"]' solution="func doesEscape() *int {\n    y := 42\n    return &y\n}\nfunc noEscape() int {\n    y := 42\n    return y\n}" explain="返回局部指针时编译器判定 y 被外部引用，必须堆分配；改成返回值后变量留在栈上。完整可编译版本见第一段代码。" />

## 随堂检测
<Quiz no="98-1" q="Go 1.23 默认的内联代价预算 inlineMaxBudget 是多少？" :options='["40","80","200","2000"]' answer="1" explain="inline/inl.go 定义 inlineMaxBudget = 80；2000 是 PGO 热点的 inlineHotMaxBudget，别把两者混淆。" />
<Predict no="98-2" q="给函数加上 //go:noinline 之后，-gcflags=-m 会怎么描述它？" :options='["仍显示 can inline", "显示 cannot inline 并注明 go:noinline", "完全不提这个函数"]' answer="1" output="cannot inline noInline: marked go:noinline" explain="编译器遵守指令，跳过内联并在 -m 输出里给出原因；这常用于写基准测试时保留真实调用开销。" />

## 一页纸速记
<KnowledgeCard :points='["编译链：AST、IR、SSA、优化 pass、机器码", "内联预算默认 80，PGO 热点可到 2000", "-m 看逃逸与内联，GOSSAFUNC 看 pass"]' :pitfalls='["把内联预算当行数", "凭感觉判断逃逸而不跑 -gcflags=-m", "在 CI 里漏掉编译期回归检查"]' summary="优化器做的决定都能被工具复现，先验证再改动，别和编译器对赌。" />

## 小结与下一讲
1. 源码经 AST、IR、SSA、优化 pass 再到机器码，SSA 的单赋值形式让优化可证明。
2. 内联按代价预算 80 决策，逃逸分析在编译期沿指针流决定栈或堆。
3. `-gcflags=-m`、`GOSSAFUNC`、`go tool compile -S` 是验证优化决策的三件工具。

[下一讲：Go 内存模型与 happens-before 精解](/advanced/memory-model)
