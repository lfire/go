---
title: 第 36 讲 · 编译、链接、SSA 与内联
description: 看懂 Go 编译产物与优化决策：SSA 中间表示、内联预算、逃逸分析输出与 objdump / nm 验证
---

<StageHeader stage="L3 · 核心机制" level="★★★★☆" time="45 分钟" goal="看懂 Go 编译产物与优化决策" prereq="第 35 讲 · 程序启动、init 顺序与包初始化" />

# 第 36 讲 · 编译、链接、SSA 与内联

> `go build` 之后那个二进制里，你的函数还在原来的样子吗？多半不在：小函数可能被内联抹掉，循环里的边界检查可能被消除，局部变量可能被挪到堆。这一讲把从源码到可执行文件的流水线拆开，并给出每条「优化结论」的验证命令，而不是让你背结论。

## 这一讲你会得到什么

<CheckList id="L36-goals" title="本讲目标" :items='["说出 go build 从源码到可执行文件的各阶段","理解 SSA 与 Phi 为何便于做优化","用内联预算解释某个函数为何没被内联","读懂 -gcflags=-m 的逃逸与内联输出","用 objdump、nm、GOSSAFUNC 验证优化结果"]' />

## 1. 先建立直觉

把编译想成一条加工流水线：源码先被拆成语法树并做类型检查，再翻译成一种每个变量只赋值一次的 IR（SSA），优化器在这上面反复打磨，之后生成汇编、汇编成目标文件，最后由链接器把成百上千个目标文件缝成一个可执行文件。每过一站，代码都可能被改写，所以「源码长什么样」和「机器跑什么」并不一致。

<ConceptDiagram type="compile-link" title="从 .go 到可执行文件" caption="前端生成 IR，优化器改写 IR，后端出汇编，链接器拼装符号与重定位" />

## 2. 语法与规则

内联是最容易亲眼验证的优化。下面故意留一个被 `//go:noinline` 禁用的函数作对照。

```go
package main

import "fmt"

//go:noinline
func add(a, b int) int { return a + b } // 强制不内联，作为对照

func double(x int) int { return x * 2 } // 成本极低，通常被内联

func main() {
	fmt.Println(add(1, 2))
	fmt.Println(double(21))
}
```

命令：`go build -gcflags=-m inline.go 2>&1 | grep inline` 看决策；再 `go build -o inline inline.go && go tool objdump -s 'main.main' ./inline | grep CALL`，已内联时能看到对 `main.add` 的调用、却看不到对 `main.double` 的调用，因为后者的函数体被铺进了调用点。

<OutputBox title="-gcflags=-m 的典型输出" output="can inline double with cost 4 as: func(int) int { return x * 2 }\ncannot inline add: marked go:noinline\nmain.main 中可见 CALL main.add，无 CALL main.double" note="cost 是内联成本估算，超过预算或含昂贵构造的函数会被拒绝内联。" />

<LineByLine code="//go:noinline\nfunc double(x int) int { return x * 2 }\nfmt.Println(double(21))" :notes='["编译指令：强制保留该函数的独立栈帧","成本极低的函数体，会被编译器内联进调用点","调用点被替换成内联后的表达式，不再产生 CALL"]' />

## 3. 深入一步

SSA（静态单赋值）要求每个变量只被赋值一次，控制流汇合处用 Phi 节点挑选来自不同分支的值。这种形式让常量传播、死代码消除、公共子表达式消除、边界检查消除（BCE）与 nil 检查消除都能用统一的数据流分析完成。用 `GOSSAFUNC=main.sum go build bce.go` 会在当前目录生成 `ssa.html`，逐 pass 展示 IR 怎么被改写。

```go
package main

import "fmt"

// 循环上界来自 len(xs)：编译器能证明下标恒合法，
// 从而消除每次迭代的边界检查（BCE）
func sum(xs []int) int {
	total := 0
	for i := 0; i < len(xs); i++ {
		total += xs[i]
	}
	return total
}

func main() {
	fmt.Println(sum([]int{1, 2, 3, 4, 5}))
}
```

命令：`GOSSAFUNC=main.sum go build bce.go` 打开生成的 `ssa.html` 对比各 pass；`go tool compile -S bce.go | grep -E 'panicIndex|main.sum'` 检查汇编里是否还有 `runtime.panicIndex`。若边界检查被消除，循环体里就看不到它。

内联的成本预算约 80 个单位（`inlineMaxBudget`，随 Go 版本调整）。含 `for`、`select`、`defer`、`go`、`recover`、闭包调用的构造要么成本高、要么直接禁止内联。它还有一层放大效应：小函数被内联后，逃逸分析与 BCE 能跨函数边界看到更多上下文，原本要上堆的变量可能被留在栈上。`go build -gcflags="-m -m" inline.go` 会打印每个调用点的成本明细。

<FlowTimeline :steps='[{"title":"语法与类型检查","desc":"源码解析成 AST，标注类型信息","tone":"gray"},{"title":"SSA 构造","desc":"转成静态单赋值 IR，插入 Phi","tone":"blue"},{"title":"优化 pass","desc":"常量传播、死代码消除、BCE、内联决策","tone":"violet"},{"title":"汇编与目标文件","desc":"lower 到平台指令，写出 .o","tone":"blue"},{"title":"链接","desc":"符号解析、重定位、生成 pclntab 与可执行文件","tone":"green"}]' direction="horizontal" />

| 命令 | 产物或输出 | 用来回答 |
| --- | --- | --- |
| `go build -gcflags=-m` | 逃逸与内联决策 | 变量是否上堆、函数是否内联 |
| `GOSSAFUNC=main.f go build` | `ssa.html` 各 pass | 优化器做了哪些变换 |
| `go tool compile -S` | 带源码行号的汇编 | 边界检查、调用是否还在 |
| `go tool objdump -s` | 反汇编指定函数 | 内联后是否还有 CALL |
| `go tool nm` | 符号表 T / R / D | 链接进了哪些代码与数据 |
| `go version -m` | 构建信息 | Go 版本与 build settings |

结论从哪来：内联预算在 `cmd/compile/internal/inline`，SSA pass 在 `cmd/compile/internal/ssa`，逃逸与 `-m` 输出在 `cmd/compile/internal/escape`。链接器的符号类型里，`T` 是代码、`R` 是只读数据、`D` 是可写数据；`go tool nm ./binary | grep 'main\.'` 能列出你写的函数是否都还在。

<Term word="SSA" text="静态单赋值中间表示，每个变量只赋值一次、汇合处用 Phi 选值，便于做统一的数据流优化。" />
<Term word="内联预算" text="编译器给函数体估算的成本上限（约 80 单位），超限或含昂贵构造就不内联。" />

<Callout type="warn" title="易错点" text="读 -gcflags=-m 时认准三种措辞：escapes to heap 与 moved to heap 表示上堆，does not escape 表示留在栈。至于「哪些写法会逃逸」的完整清单在第 27 讲，本讲只关注怎么读输出、怎么验证优化是否生效。" />

## 4. 工程实践

一段返回指针的小函数，是同时观察逃逸输出、符号表与反汇编的好样本。

```go
package main

import "fmt"

type point struct{ X, Y int64 }

// 返回局部变量指针：p 逃逸到堆，编译后调用 runtime.newobject
func newPoint(x, y int64) *point {
	return &point{X: x, Y: y}
}

func main() {
	p := newPoint(3, 4)
	fmt.Println(p.X, p.Y)
}
```

命令：`go build -gcflags=-m escape_out.go 2>&1 | grep -E 'escape|newobject'` 看逃逸结论；`go build -o escape_out escape_out.go && go tool nm ./escape_out | grep -E 'main.newPoint|runtime.newobject'` 核对符号；`go tool objdump -s 'main.newPoint' ./escape_out | grep CALL` 看分配调用是否真的存在；`go version -m ./escape_out` 查构建信息。

<Compare :panels='[{"label":"难以内联","code":"func handle(r *Req) error {\n// 含 select、defer、闭包等\nreturn nil\n}","note":"成本高或含昂贵构造，保留独立栈帧"},{"label":"利于内联","code":"func short(x int) int { return x + 1 }\nfunc handle(r *Req) error {\nreturn use(short(r.N))\n}","note":"小函数被内联，逃逸与 BCE 可跨函数生效"}]' title="用函数形状配合优化器" />

工程上的次序是：先写出清晰的小函数，再用 `-gcflags=-m` 与 `GOSSAFUNC` 确认优化器确实帮了忙（或没帮上），最后才考虑 `//go:noinline` 这类人工干预。盲目加编译指令、或为了内联把逻辑揉成一团，往往得不偿失。链接器默认做静态链接，例外是启用 cgo 时可能动态依赖 libc，那是下一讲的边界。

## 动手练一练

<Exercise title="验证一次边界检查消除" task="对 sum 函数生成 ssa.html，找出执行 BCE 的那个 pass，再用 go tool compile -S 确认汇编里没有 panicIndex；随后把循环改成下标可能越界的写法，对比结果。" :hints='["GOSSAFUNC=main.sum go build bce.go 会生成 ssa.html","grep panicIndex 检查边界检查","改成 i 的上界与 len 无关的写法会重新引入检查"]' solution="GOSSAFUNC=main.sum go build bce.go\ngo tool compile -S bce.go | grep panicIndex\n原写法：无 panicIndex（BCE 生效）\n越界写法：出现 CALL runtime.panicIndex" explain="编译器只有在能静态证明下标合法时才会消除边界检查，证明依赖 SSA 上的范围分析。" />

## 随堂检测

<Quiz no="36-1" q="想看某个函数被优化器逐 pass 改写的全过程，应该用哪条命令？" :options='["go vet","GOSSAFUNC=main.f go build","go build -gcflags=-m","go tool nm"]' answer="1" explain="GOSSAFUNC 指定函数后，编译器把各轮 SSA pass 的 IR 写进 ssa.html，可逐步对比优化前后。" />
<Predict no="36-2" q="一个小函数因为含 select 和 defer 而没被内联，最可能的原因是什么？" :options='["函数返回了指针","内联成本超预算或命中禁止内联的构造","它没有 import","目标平台不支持"]' answer="1" output="内联被拒绝，函数保留独立栈帧并产生 CALL" explain="内联预算约 80 单位，select、defer、go、recover 等构造成本高或直接禁止内联。" />

## 一页纸速记

<KnowledgeCard :points='["流程：语法与类型检查 → SSA → 优化 pass → 汇编与 .o → 链接","SSA 每变量只赋一次、用 Phi 合并，便于常量传播与 BCE","内联预算约 80 单位，内联还能放大逃逸分析与边界检查消除的效果","-gcflags=-m 读逃逸与内联，objdump / nm / GOSSAFUNC 做验证"]' :pitfalls='["只凭源码猜测优化是否发生，不做命令验证","为内联揉乱代码，或随处加 go:noinline 干扰优化器"]' summary="编译产物与源码并不一致，优化结论要用 -m、ssa.html 与反汇编逐条核实。" />

## 小结与下一讲

- SSA 让各类优化有了统一的载体，`GOSSAFUNC` 能把它可视化。
- 内联是「小函数免费、还能带动其它优化」的关键，但受预算与构造限制。
- 逃逸、内联、边界检查都能用命令验证，别凭感觉调优。

编译器也留了一道绕过类型系统的后门，代价与边界正是最后一讲。[下一讲：unsafe、cgo 与跨边界成本](/core/unsafe-cgo)
