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

### 源码精读与可复现验证（以 Go 1.21~1.23 为准，版本可能变化）

编译器的目录划分本身就是一条阅读路线，全部在 `src/cmd/compile/internal/` 下：`noder` 与 `types2` 把源码解析成 AST 并做类型检查，`ir` 是统一的中间表示节点，`inline` 负责内联，`escape` 负责逃逸分析，`ssagen` 把 IR 构造成 SSA，`ssa` 里放着优化 pass 与 `compile.go` 的 pass 列表，`obj` 产出目标文件。内联发生在 SSA 之前，`src/cmd/compile/internal/inline/inl.go` 定义 `inlineMaxBudget = 80` 与 `inlineHotMaxBudget = 2000`，按「代价节点数」而非源码行数计分，调用、闭包、可变参数、`defer` 都会额外加价；Go 1.21 起的 PGO 会把热点函数的预算抬到 2000。逃逸分析在 `escape/escape.go` 里沿指针流建图，给每个局部变量标一个「逃逸位置」，结论直接决定生成栈上分配还是 `runtime.newobject`。

SSA 部分读两处就够：`ssa/compile.go` 的 pass 列表决定了优化顺序，`GOSSAFUNC` 环境变量会让编译器把每个 pass 前后的 SSA 图写进当前目录的 `ssa.html`；`ssa/prove.go` 是基于 SSA 的证明器，边界检查消除（BCE）就发生在这里——当它能证明下标由 `range` 约束时，对应的 `IsInBounds` 检查会被删掉。`ssagen` 最后调 `genssa` 选指令、分寄存器、产出汇编。一个可复现的观察实验：对第 3 段程序跑 `GOSSAFUNC=sum1 go build -o /dev/null .`，在生成的 `ssa.html` 里对比 `prove` pass 前后的值节点数；再 `go tool compile -S` 数 `sum1` 与 `sum2` 里 `runtime.panicIndex` 的出现次数，`sum1` 应为 0，`sum2` 仍保留显式范围判断，这就把「SSA 让优化可证明」落成了可数的证据。

适用边界要写清：以上目录、常量与 pass 名以 Go 1.21~1.23 为准，pass 列表与 `-gcflags=-m` 的输出文案每版都可能调整，判断内联要以 `can inline` 与 `inlining call to` 为准而不是套某句固定文本；内联预算是代价节点数，别当函数行数；PGO 结论只在带 `default.pgo` 的构建里成立；Go 1.17 起 amd64 等平台用寄存器 ABI，汇编里参数走 AX、BX 而非 `FP` 偏移，拿旧教程的栈帧图对照会对不上。

一条命令把编译器最终生成的汇编打出来：

```bash
go tool compile -S add.go
```

```text
main.add STEXT nosplit size=19 args=0x10 locals=0x0 funcid=0x0 align=0x0
        0x0000 00000 (add.go:3) TEXT main.add(SB), NOSPLIT|ABIInternal, $0-16
        0x0000 00000 (add.go:3) FUNCDATA $0, gclocals·w1C0hLTfU0e5E9yGk8SUA==(SB)
        0x0000 00000 (add.go:3) FUNCDATA $1, gclocals·w1C0hLTfU0e5E9yGk8SUA==(SB)
        0x0000 00000 (add.go:3) ADDQ BX, AX
        0x0003 00003 (add.go:3) RET
```

首行是符号摘要：`main.add` 是符号名，`STEXT` 表示代码段，`nosplit` 说明小函数不插入栈增长检查；`size=19` 是机器码字节数，`args=0x10` 是两个 int 参数加返回值共 16 字节，`locals=0x0` 表示没有栈上局部变量，`funcid` 与 `align` 是函数标识与对齐要求。之后每行前三列分别是函数内偏移（如 `0x0000`）、全局字节偏移（如 `00000`）与源码行号 `(add.go:3)`，用来把汇编逐行对回源码。`TEXT ... NOSPLIT|ABIInternal, $0-16` 是入口伪指令，`SB` 是静态基址寄存器，`$0-16` 表示本地帧 0 字节、参数区 16 字节；两条 `FUNCDATA` 是 GC 需要的栈指针与活跃指针映射。`ADDQ BX, AX` 把两个参数寄存器相加、结果留在 AX，说明寄存器 ABI 下参数根本不上栈；末尾 `RET` 之后没有任何 `CALL`，正好佐证这个 `add` 既没内联失败、也没触发 `runtime.newobject`。把第 2 段的 `doesEscape` 拿去编译，就会看到 `CALL runtime.newobject(SB)` 出现，那是逃逸落到堆上的铁证。

<SequenceDiagram title="一个函数从源码到汇编的编译链" caption="内联与逃逸在 SSA 之前完成，prover 负责消除可证明的边界检查" :actors='["go build","noder","inline/escape","ssagen","ssa pass","obj"]' :steps='[{"from":"go build","to":"noder","label":"解析成 AST","note":"含 import 与类型检查","kind":"call"},{"from":"noder","to":"inline/escape","label":"类型检查得 IR","note":"统一 IR 节点","kind":"call"},{"from":"inline/escape","to":"inline/escape","label":"定内联与逃逸","note":"按预算 80 决策","kind":"self"},{"from":"inline/escape","to":"ssagen","label":"交 IR 构造 SSA","note":"buildssa 单赋值","kind":"call"},{"from":"ssagen","to":"ssa pass","label":"逐 pass 化简","note":"按 compile.go 顺序","kind":"call"},{"from":"ssa pass","to":"ssa pass","label":"BCE 与常量折叠","note":"prove 证明性质","kind":"self"},{"from":"ssa pass","to":"obj","label":"选指令分寄存器","note":"genssa 产出汇编","kind":"call"},{"from":"obj","to":"go build","label":"返回目标文件","note":"产出 .o","kind":"return"},{"from":"go build","to":"go build","label":"链接写二进制","note":"交给 linker","kind":"self"}]' />

图中把内联与逃逸放在 SSA 之前是关键：它们先决定「哪些代码存在、哪些变量上堆」，SSA pass 才能在已经定型的图上做证明，顺序反过来优化就无从下手。

| 观测命令 | 观察对象 | 输出里的关键判据 | 能回答的问题 |
| --- | --- | --- | --- |
| `go build -gcflags=-m` | 内联与逃逸决策 | `can inline`、`escapes to heap` | 这个函数被摊平了吗、变量上堆了吗 |
| `go build -gcflags=-m=2` | 完整决策链 | `moved to heap` 及原因 | 是哪一个引用让变量逃逸 |
| `GOSSAFUNC=sum1 go build` | 每个 SSA pass | `ssa.html` 各阶段值节点数 | `prove` 之后少了哪些检查 |
| `go tool compile -S` | 生成的汇编 | `runtime.panicIndex`、`newobject` | BCE 是否生效、有没有堆分配调用 |
| `go tool objdump -s main.sum1 ./bin` | 链接后机器码 | 真实指令地址与符号 | 最终二进制里还剩什么 |

五条命令从决策链到最终产物层层收窄，接进 CI 时通常只留 `-m` 与 `compile -S` 两条做回归门禁，其余按需手动查。

<Callout type="tip" title="源码阅读建议" text="先用 GOSSAFUNC=sum1 go build 生成 ssa.html，从左到右点几个 pass 看值节点怎么变少，再回头读 ssa/compile.go 的 pass 列表；内联与逃逸先各读一个函数就够，别一上来啃 inl.go 的全部代价表。记 pass 的效果比记 pass 的名字更耐用。" />

### 常见误读与反例

编译器优化的误读大多来自「把编译期决策当成运行期行为」或「把一个工具的结论外推到全部代码」。下面三条用 `-gcflags=-m` 与 `go tool compile -S` 就能推翻。

| 误读 | 为什么站不住（源码推导） | 正确写法或反例 |
| --- | --- | --- |
| 「内联预算 80 是 80 行代码，函数短就一定被内联」 | `inline/inl.go` 的 `inlineMaxBudget = 80` 按**代价节点数**计分，调用、闭包、可变参数、`defer` 都额外加价，`//go:noinline` 会直接跳过；PGO 的 2000 也只对带 `default.pgo` 的热点函数生效 | 以 `go build -gcflags=-m` 输出的 `can inline` 与 `inlining call to` 为准，别数行数；第 2 段程序里 `add` 与 `noInline` 的对照就是现成反例 |
| 「返回指针一定比返回值慢，逃逸了就说明代码有问题」 | `doesEscape` 返回局部指针触发 `runtime.newobject`，但小结构体返回值也可能因接口装箱、闭包捕获而逃逸，大结构体返回指针反而能避免整块拷贝。逃逸分析结论只是「是否堆分配」，不是「好坏」 | 用 `-gcflags=-m=2` 看清是哪个引用导致 `moved to heap`，再用 `go test -benchmem` 看 `B/op` 与 `allocs/op`；第 4 节 `Compare` 的两栏要配合基准数字读，不能只看返回类型 |
| 「SSA 是运行时优化，汇编里没有 `runtime.panicIndex` 就证明源码不会越界」 | SSA 是**编译期** IR，`ssa/prove.go` 的 BCE 只删掉「可证明」的检查（如 `range` 约束的下标）；它证明的是编译器能推导，不代表所有路径都安全，也不代表链接后二进制完全一样 | `go tool compile -S` 看 `sum1` 里 `runtime.panicIndex` 是否消失，再用 `go tool objdump -s main.sum1 ./bin` 确认最终产物；两个工具一个看编译结果、一个看链接结果，不能互相替代 |

<Quiz q="go tool compile -S 的输出里没有 CALL runtime.newobject，能证明什么？" :options='["这个函数运行期绝不分配堆内存","这个编译单元里该函数没有生成堆分配调用","整个程序零分配","链接后的二进制也没有分配"]' answer="1" explain="compile -S 只显示当前包编译出的汇编，没有 newobject 说明该函数这次编译未生成堆分配调用；跨包调用、内联进调用方以及链接后的产物都要另查，不能外推到整个程序。" />

这三条有同一个根源：把某个观测手段能证明的范围当成了它能证明的全部。`-m` 证明「编译器这次怎么决策」，`compile -S` 证明「这个包生成了什么汇编」，`objdump` 证明「链接后还剩什么指令」，三者层层收窄，却没有一个能单独证明运行时行为——这正是第 3 节那张观测命令表按「观察对象」分列的原因。

### 与 L10 源码级实战的呼应

- [实战 02 · protoc 插件与代码生成器](/projects/cli-codegen)：生成的错误码注册代码若不小心返回指针或把值装箱成 `interface{}`，会让本可栈分配的对象逃逸；把 `-gcflags=-m` 接进「生成与格式化」之后的检查，就多了一道编译期质量门禁。
- KV 贯穿线 ① [内存版与接口抽象](/projects/kv-1-memory)：内存版 Storage 的基准测试要用 `-benchmem` 看 `allocs/op`，本讲的逃逸分析正是解释「每次 Get 是否多一次分配」的工具；接口抽象带来灵活性的同时是否引入装箱，也靠它判断。
- KV 贯穿线 ② [TCP 协议帧与网络层](/projects/kv-2-network)：协议帧编解码是热路径，`go tool compile -S` 里若出现 `runtime.newobject` 就说明帧对象逃逸；BCE 是否生效决定逐字节解析还剩几条边界检查，直接影响这条网络层的吞吐上限。

## 4. 工程实践
写热路径代码时，与其猜编译器有没有优化，不如让编译器自己交代。`GOSSAFUNC=main go build` 会在当前目录生成 `ssa.html`，逐 pass 展示 SSA 图如何被化简；`go tool compile -S` 打印汇编，可数出循环里还剩几条边界检查。把这些命令接进 CI，能在性能回归合并之前就发现「某个改动意外让变量逃逸」这类问题。

```bash
GOSSAFUNC=main go build -o /dev/null .
go tool compile -S main.go
```

<Compare title="返回指针还是返回值" :panels='[{"label":"写法 A：返回指针","code":"func f() *User {\n    u := new(User)\n    return u\n}","note":"对象逃逸到堆，每次调用一次分配，给 GC 增加压力"},{"label":"写法 B：返回值","code":"func f() User {\n    return User{}\n}","note":"小结构体留在栈或寄存器里，零堆分配，用 -gcflags=-m 可验证"}]' />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l9-advanced/lesson-098-compiler-ssa`](https://github.com/lfire/go/tree/main/examples/l9-advanced/lesson-098-compiler-ssa)。clone 仓库后进入 `examples/l9-advanced`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

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
