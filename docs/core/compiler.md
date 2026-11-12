---
title: 第 036 讲 · 编译、链接、SSA 与内联
description: 看懂 Go 编译产物与优化决策：SSA 中间表示、内联预算、逃逸分析输出与 objdump / nm 验证
---

<StageHeader stage="L3 · 核心机制" level="★★★★☆" time="45 分钟" goal="看懂 Go 编译产物与优化决策" prereq="第 035 讲 · 程序启动、init 顺序与包初始化" />

# 第 036 讲 · 编译、链接、SSA 与内联

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

<Callout type="warn" title="易错点" text="读 -gcflags=-m 时认准三种措辞：escapes to heap 与 moved to heap 表示上堆，does not escape 表示留在栈。至于「哪些写法会逃逸」的完整清单在第 027 讲，本讲只关注怎么读输出、怎么验证优化是否生效。" />

### 推导链：从 cmd/compile 各阶段到 SSA 上的边界检查消除

把「源码不等于机器码」落到具体目录：`cmd/compile/internal/syntax` 与 `noder` 负责解析，`types2`/`typecheck` 做类型检查，`ir` 承载中间表示，`ssa` 跑优化 pass，`ssagen` 把 IR 降级成平台相关代码，`obj` 输出汇编目标文件，最后 `cmd/link` 做符号解析、重定位并生成 `pclntab`。SSA 构造阶段插入 `Phi`，随后 `prove` pass 做范围分析支撑 BCE（边界检查消除）、`cse` 做公共子表达式消除、`dce` 做死代码消除、`nilcheckelim` 做 nil 检查消除；内联成本预算约 80（相关常量在 `cmd/compile/internal/inline`，历史名 `inlineMaxBudget`，不同版本重构过）。推导链是：`for i := 0; i < len(xs); i++` 让 `prove` 能证明 `i` 恒在 `[0, len(xs))` 内，于是 `xs[i]` 的 `runtime.panicIndex` 被删掉；若把上界换成与 `len(xs)` 无关的变量，证明失败，检查重新出现。

用构建信息与符号表把「优化决策」与「链接产物」对上。把第 2 节的 `inline.go` 编译：

```bash
go build -o inline inline.go
go version -m ./inline
go tool nm -size ./inline | grep -E ' main\.' | head -8
# 部分版本支持：让编译器打印哪些边界检查被消除
go build -gcflags='-d=ssa/check_bce/debug=1' bce.go 2>&1 | grep -i isinbounds || true
```

<OutputBox title="go version -m 输出节选（Go 1.23 / darwin arm64）" output="./inline: go1.23.8\n    path    command-line-arguments\n    build   -buildmode=exe\n    build   -compiler=gc\n    build   CGO_ENABLED=1\n    build   GOARCH=arm64\n    build   GOOS=darwin" note="此处为节选，完整输出还含多行 CGO_* 设置；build 行记录平台与 cgo 前提。" />

逐字段解释：首行 `go1.23.4` 是编译器版本，直接决定本讲所有优化能力；`build` 行里 `GOARCH`/`GOOS`/`CGO_ENABLED` 是平台与 cgo 前提，`-compiler=gc` 说明用的正是这套编译器。`go tool nm -size` 的三列是地址、符号类型与「函数名加字节大小」，`T` 是代码、`R` 是只读数据、`D` 是可写数据；在 `inline` 这个样本里通常只列出 `main.main` 与被 `//go:noinline` 强行保留的 `main.add`，而 `main.double` 因被完全内联且无其它引用，不再作为独立符号出现——把 `-gcflags=-m` 的「can inline」与链接产物对上了。`-d=ssa/check_bce/debug=1` 若被支持，会打印 `Found IsInBounds` 之类的行，指出具体哪一行的边界检查被消除。

**为什么这样设计**：SSA 让「每个变量只赋一次加 Phi 合并」把控制流与数据流显式化，常量传播、CSE、DCE、BCE 能在同一套数据流框架里完成，不必为每种优化各写一遍遍历；代价是构造 SSA 与多轮 pass 增加编译时间。内联预算约 80 是「编译时间与二进制体积」对「运行时收益」的取舍：无限内联会让二进制膨胀、指令缓存命中下降、编译变慢，过小则错失跨函数逃逸分析与 BCE 的连带收益。静态链接把 runtime 与依赖一起打进单个二进制，部署简单、无动态库版本地狱，代价是体积与无法共享 libc（启用 cgo 时例外）。替代方案（JIT 或解释执行）能按运行时 profile 优化，但要带运行时编译器、预热成本与冷启动延迟，与 Go「秒级编译加快速启动」的定位冲突。

### 内联成本的逐节点算式与 80 预算的真实边界

上一节把内联当成「能 / 不能」的开关读，这一节把开关背后的算式拆开。`cmd/compile/internal/inline/inl.go` 里三个常量决定结论：`inlineMaxBudget = 80` 是基准预算，`inlineExtraCallCost = 57` 是每遇到一次调用额外加的成本，`inlineExtraThrowCost = inlineMaxBudget` 让含 `panic` 或 `throw` 的函数几乎必然超限。`CanInline` 调 `inlineCost` 遍历函数 IR：普通节点各记约 1 单位，每出现一个调用节点再加 57，累加得到 `fn.Inl.Cost`；`inlineBudget(fn)` 在 80 基准上按 PGO 热度放大；最后比较 cost 是否不超过 budget，命中就打 `can inline f with cost N as: ...`，超了打 `cannot inline f: function too complex: cost N exceeds budget 80`。调用点随后出现 `inlining call to f`，链接产物里该函数的独立符号消失——这就是「预算 → 决策 → 产物」的完整因果链。

用三个只差一次调用的函数把算式代入：

```go
package main

import "fmt"

func tiny(x int) int { return x + 1 }

func withCall(x int) int { return len(fmt.Sprint(x)) }

func twoCalls(x int) int { return len(fmt.Sprint(x)) + len(fmt.Sprint(x)) }

func main() {
	fmt.Println(tiny(1), withCall(2), twoCalls(3))
}
```

```bash
go build -gcflags='-m -m' -o /dev/null inline_cost.go 2>&1 | grep -E 'can inline (tiny|withCall|twoCalls)|cannot inline (tiny|withCall|twoCalls)'
```

<OutputBox title="-m -m 逐函数成本（Go 1.23 / arm64）" output="can inline tiny with cost 4 as: func(int) int { return x + 1 }\ncan inline withCall with cost 65 as: ...\ncannot inline twoCalls: function too complex: cost 130 exceeds budget 80" note="一次调用加 57：65 = 8 + 57，130 = 16 + 114；只差一次调用就越过 80。" />

逐字段解释：`tiny` 只有 `x+1` 与返回，约 4 个节点，cost 4；`withCall` 多一次 `fmt.Sprint` 调用，`8 + 57 = 65`，仍低于 80，所以**含函数调用照样能内联**；`twoCalls` 两次调用，`16 + 57×2 = 130`，超过 80 被拒，诊断里直接写出 `cost 130 exceeds budget 80`。把 `tiny` 那种纯累加语句堆到 25 行，cost 会线性涨到 82——只超预算 2 就被拒，65 行则到 202；这说明成本是「节点数加 57 乘调用次数」的线性叠加，而不是看源码行数。诊断末尾还常伴随 `inlining call to tiny` 与 `inlining call to withCall`，把决策与调用点对上。

**反直觉结论的证伪**：「函数越短越容易内联、含调用就一定不内联」都不对。对照数据里 `withCall` 只有一行却含调用、cost 65 仍能内联；反过来一个 25 行、零调用的纯算术函数 cost 82，反而因超预算被拒。另一个易错点是「`-gcflags=-m` 里没看到某函数，说明它没被编译」——恰恰相反，`cost 130 exceeds budget 80` 之后函数保留独立 TEXT 段，`go tool nm -size | grep twoCalls` 能找到它，而完全内联且无其它引用的 `tiny` 反而可能不作为独立符号出现。预算也不是铁板一块：Go 1.21 起 PGO 会对热点函数放大 `inlineBudget`，同一份代码带不带 `default.pgo` 可能得到不同结论；`-gcflags=-m` 的措辞随版本重构，脚本解析它不可靠。

边界：`inlineMaxBudget` 与 `inlineExtraCallCost` 是 Go 1.23 的取值，历史上多次调整；`inlineExtraThrowCost` 让含 `panic` 的函数基本不内联；`-gcflags` 默认只作用于当前包，看标准库要用 `all=-m`；`//go:noinline` 直接跳过成本计算（日志写 `marked go:noinline`），`//go:linkname` 等指令也不保证跨版本稳定；成本模型与 `GOARCH` 无关，但内联后的逃逸分析、BCE 与寄存器分配随平台变化，最终 `ns/op` 必须本机实测。

### SSA pass 的真实执行顺序：`-d=ssa/all/time` 把它打印出来

前面用 `GOSSAFUNC` 看过 `ssa.html` 里各 pass 的 IR，但那是按函数生成的静态快照；编译器内部其实按 `cmd/compile/internal/ssa/compile.go` 的 `passes` 数组**线性跑一遍**，顺序本身带着约束。数组里每个元素有 `name`、`fn`、`required` 三个字段，`required: true` 的 pass 不可跳过（`opt`、`decompose user`、`expand calls`、`lower`、`late lower`、`regalloc`、`schedule`、`layout`、`tighten` 等）；`required: false` 的是优化项，跳过只影响质量不影响正确性。把关键片段按顺序列出来：

```text
number lines → early phielim and copyelim → early deadcode → short circuit
→ decompose user → pre-opt deadcode → opt → zero arg cse → opt deadcode
→ generic cse → phiopt → gcse deadcode → nilcheckelim → prove → early fuse
→ expand calls → decompose builtin → softfloat → late opt → dead auto elim
→ sccp → generic deadcode → check bce → branchelim → late fuse → dse
→ memcombine → writebarrier → insert resched checks → lower → addressing modes
→ late lower → lowered deadcode for cse → lowered cse → ... → tighten
→ late deadcode → critical → phi tighten → likelyadjust → layout → schedule
→ late nilcheck → flagalloc → regalloc → loop rotate → trim
```

顺序不是随意排的，至少有三处可推的因果：`prove` 必须排在 `check bce` **之前**，因为边界检查消除要先由 `prove` 做范围分析、`check bce` 只是把「还剩哪些检查」报出来；`writebarrier`（`required`）必须排在 `lower` 之前，它把屏障 Op 展开成真正的写屏障调用，`lower` 才能把展开后的 Op 降级成平台指令；`layout`（排基本块）与 `schedule`（排指令）在 `flagalloc`、`regalloc` **之前**——Go 先定指令顺序再分配寄存器，这与很多教材里「先分配寄存器再调度」的直觉相反，也解释了为什么 `ssa.html` 里最后几轮 pass 的 IR 已经很接近机器码。`sccp`（稀疏条件常量传播）排在 `late opt` 之后，则是因为前面几轮 `cse`/`phiopt`/`fuse` 已经把常量条件暴露出来，再跑一次收益最大。

新观察角度：让编译器**按执行顺序打印每个 pass 的耗时**。`-d=ssa/all/time` 是 `ssa.PhaseOption` 支持的内部开关，输出顺序就是真实执行顺序，比读 `ssa.html` 的目录更直接；再用 `-d=ssa/prove/debug=1` 单独看 `prove` 这一轮做了什么。用第 3 节的 `bce.go`（`sum(xs []int)` 那个程序）即可：

```bash
go build -gcflags='-d=ssa/all/time' -o /dev/null bce.go 2>&1 | grep 'sum' | head -30
go build -gcflags='-d=ssa/prove/debug=1' -o /dev/null bce.go 2>&1 | head -6
sed -n '457,509p' "$(go env GOROOT)/src/cmd/compile/internal/ssa/compile.go"
```

<OutputBox title="sum 函数的 SSA pass 顺序与耗时（Go 1.23.8 / arm64，节选）" output="number_lines                 TIME(ns)     1833  sum\nearly_phielim_and_copyelim   TIME(ns)     1250  sum\nopt                          TIME(ns)     4166  sum\ngeneric_cse                  TIME(ns)     3958  sum\nnilcheckelim                 TIME(ns)     2292  sum\nprove                        TIME(ns)    10709  sum\nexpand_calls                 TIME(ns)     5084  sum\nlate_opt                     TIME(ns)     2417  sum\nsccp                         TIME(ns)     7000  sum\ncheck_bce                    TIME(ns)       41  sum\nwritebarrier                 TIME(ns)      583  sum\nlower                        TIME(ns)     5084  sum\ntighten                      TIME(ns)    12875  sum\nlayout                       TIME(ns)      833  sum\nschedule                     TIME(ns)     2875  sum\nflagalloc                    TIME(ns)     1042  sum\nregalloc                     TIME(ns)    17458  sum\n--- -d=ssa/prove/debug=1 ---\n./bce.go:5:21: Induction variable: limits [0,?), increment 1\n./bce.go:5:40: Proved IsInBounds" note="输出行的先后即 pass 执行顺序；prove 报出归纳变量与 IsInBounds，check_bce 紧随其后。" />

逐字段解释：每行第二列是 pass 名（下划线版）、`TIME(ns)` 是该 pass 在这台机器上的纳秒耗时、末列是函数名；把这份输出与 `sed -n '457,509p'` 的 `passes` 数组逐行对照，顺序完全一致。`prove` 的 10709 ns 明显高于邻近 pass，因为它要做范围分析；`-d=ssa/prove/debug=1` 打印的 `Induction variable: limits [0,?), increment 1` 与 `Proved IsInBounds` 正是它证明 `i` 恒在 `[0, len(xs))` 的证据，紧接着的 `check_bce` 只花 41 ns、且不再报出未消除的检查——「prove 先、check bce 后」的因果链在同一份输出里同时可见。`regalloc` 的 17458 ns 与 `schedule`/`flagalloc` 在其之前，也把「先调度后分配寄存器」坐实。注意 `-d=ssa/all/time` 的耗时随机器与函数波动、只作顺序证据，不做性能结论；这是编译器内部开关，跨版本可能改名。

### PGO 的 `BudgetExpansion`：80 怎样被放大到 160 与 2000

上一版量过 `inlineMaxBudget = 80` 与「一次调用加 57」的成本算式，但只把 PGO 当成一句「热点函数会放大预算」。放大其实有**两个不同的系数**，源码在 `cmd/compile/internal/inline/inl.go` 与 `inline/inlheur/scoring.go`（Go 1.23.8）：

```text
inlineBudget(fn, profile, relaxed, verbose):
    budget = inlineMaxBudget                 // 80
    若 IsPgoHotFunc(fn, profile) → budget = inlineHotMaxBudget   // 2000，热点函数
        verbose 时打印 hot-node enabled increased budget=2000 for func=...
    若 relaxed → budget += inlheur.BudgetExpansion(inlineMaxBudget)

BudgetExpansion(maxBudget):
    若 base.Debug.InlBudgetSlack 不为 0 → 返回它（可用 -d=inlbudgetslack 覆盖）
    否则返回 maxBudget                    // 默认 80，即把预算从 80 放宽到 160
```

`scoring.go` 的注释把动机写清了：新内联器会在 `RevisitInlinability` 里按调用点启发式把某些 callee 的评分**往下调**，但这个调整发生在 `CanInline` 之后，所以初次预算要预先放宽、留出余量，后面再逐个降级；默认「返回 maxBudget」正好把 80 加倍到 160。另一个系数 `inlineHotMaxBudget = 2000` 由 `PGOInlinePrologue` 通过 profile 的调用边权重 CDF（默认 `inlineCDFHotCallSiteThresholdPercent = 99`）填 `candHotCalleeMap` 后启用，并可用 `-d=pgoinlinebudget=N` 改写。两者作用在不同路径：`2000` 是「热点函数」的固定天花板，`160` 是「通用放宽量」，别混成一个数。

新观察角度：用 `-d=pgodebug=1` 与 `-d=pgoinlinebudget=N` 把放大系数**直接打印并改掉**，再用 `-d=dumpinlcallsitescores=1` 看调用点评分。复用[第 027 讲](/core/escape-analysis)那个 `pgofinal` 包（`helper` 成本 97、`BenchmarkHot` 生成 `default.pgo`）：

```bash
go test -bench=. -benchtime=300000x -cpuprofile=default.pgo -run=^$ .
go build -pgo=default.pgo -gcflags='-d=pgodebug=1' -o /dev/null .
go build -pgo=default.pgo -gcflags='-d=pgodebug=1,pgoinlinebudget=160' -o /dev/null .
go build -pgo=default.pgo -gcflags='-d=pgodebug=1,pgoinlinebudget=90' -o /dev/null .
go build -pgo=default.pgo -gcflags='-d=dumpinlcallsitescores=1' -o /dev/null .
mkdir -p "$PWD/jdiag"
go build -pgo=default.pgo -gcflags="-json=0,$PWD/jdiag" -o /dev/null .
grep -h 'cost' jdiag/pgofinal/lib.json
sed -n '247,262p' "$(go env GOROOT)/src/cmd/compile/internal/inline/inl.go"
sed -n '591,610p' "$(go env GOROOT)/src/cmd/compile/internal/inline/inlheur/scoring.go"
```

<OutputBox title="PGO 放大系数与阈值实测（Go 1.23.8 / arm64）" output="--- 默认 ---\nhot-node enabled increased budget=2000 for func=pgofinal.helper\ncanInlineFunction | cost: 97\n--- pgoinlinebudget=160 ---\nhot-node enabled increased budget=160 for func=pgofinal.helper\nhot-budget check allows inlining for call pgofinal.helper (cost 97) at ./lib.go:43:14 in function pgofinal.Hot\n--- pgoinlinebudget=90 ---\nhot-node enabled increased budget=90 for func=pgofinal.helper\n（无 allows inlining 行）\n--- dumpinlcallsitescores ---\n# Score  Adjustment  Status  Callee  CallerPos Flags ScoreFlags\n92  -5  ---|PGO  ·helper  lib.go:43:14|0  inLoopAdj" note="把天花板从 2000 改成 160、再改成 90，cost 97 恰好卡在 90 与 160 之间。" />

逐字段解释：默认 `budget=2000` 远超 `cost 97`，所以 `canInlineFunction | cost: 97` 通过；`-d=pgoinlinebudget=160` 把天花板改成 160，仍大于 97，于是多打印 `hot-budget check allows inlining for call ... (cost 97)` 并给出调用点 `lib.go:43:14` 与函数 `pgofinal.Hot`；改成 90 后 97 大于 90，这行消失——放大系数的**阈值作用**被两次构建直接夹出来。`-d=dumpinlcallsitescores=1` 的表头是 `Score / Adjustment / Status / Callee / CallerPos / ScoreFlags`，`92 -5 ---|PGO ... inLoopAdj` 说明该调用点原始评分经 `-5`（`inLoopAdj`，循环内启发式调整）后为 92，`|PGO` 标记它受 profile 影响；`---` 表示本次调整没有跨过阈值。`sed` 两段分别给出 `inlineBudget` 的 `budget = inlineHotMaxBudget` / `budget += BudgetExpansion` 与 `BudgetExpansion` 的「把 80 放宽到 160」原文，把打印出的数字接回算式。

边界：`inlineHotMaxBudget = 2000`、`inlineMaxBudget = 80`、`BudgetExpansion` 默认返回 `maxBudget`、99% CDF 阈值都是 Go 1.23 取值，历史上多次调整；`-d=pgodebug`、`-d=pgoinlinebudget`、`-d=inlbudgetslack`、`-d=dumpinlcallsitescores` 全是编译器内部开关，跨版本可能改名或移除；`default.pgo` 必须与构建包路径匹配，profile 无热点边时不会放大；`RevisitInlinability` 的降级发生在 `CanInline` 之后，所以「放宽到 160」不等于「最终一定内联」；`-d=ssa/all/time` 的耗时、`-d=dumpinlcallsitescores` 的评分都随机型与机器波动，只作顺序与相对关系证据。这条链在第 098 讲（编译器源码）会与 `interleaved.DevirtualizeAndInlinePackage` 的主流程合流。

### 边界、常见误解与前后联系

| 常见误解 | 正解 | 可验证依据 |
| --- | --- | --- |
| `go build -gcflags=-m` 的输出是稳定 API，可写脚本解析 | 诊断措辞随版本变（`escapes to heap`、`moved to heap`、`does not escape` 同类），只用于人工核对 | 换一个 Go 版本重跑同一命令 |
| 内联后函数符号还在二进制里 | 被完全内联且无其它引用的函数，其独立符号可能不再出现 | `go tool nm -size | grep main.` 的结果 |
| 看了 `ssa.html` 就等于看到最终机器码 | SSA 之后还有 lower、汇编、链接与调度，最终指令可能再变 | `GOSSAFUNC` 与 `go tool objdump` 对照 |

边界要点：Go 1.17 起 amd64 等平台改用寄存器 ABI，汇编里参数位置与旧资料不同；Go 1.20 起支持 PGO（`-pgo`，Go 1.21 起默认读取 `default.pgo`），内联与去虚拟化决策会因 profile 变化；`-gcflags` 默认只作用于当前包，要观察标准库优化得用 `-gcflags=all=-m`；`GOSSAFUNC` 生成的 `ssa.html` 只覆盖指定函数、不含链接与调度后的结果；`//go:noinline`、`//go:linkname` 等编译指令不保证跨版本稳定；`go tool nm` 的符号可能因死代码消除而缺失，别把「符号不在」直接当成「函数没写」；观测 BCE 的 `-d=ssa/check_bce/debug` 属于编译器内部开关，不同版本可能改名或移除。

前后联系：第 027 讲（逃逸分析）与本讲共用 `-gcflags=-m`，第 098 讲（编译器源码：SSA、逃逸与内联）会精读 SSA 构造、优化 pass、内联预算与逃逸分析实现，是本讲最直接的源码延续；第 100 讲（极致性能优化）讲 PGO 与 profile 驱动内联在真实调优里的位置；第 099 讲（Go 内存模型与 happens-before）虽不直接讲编译，但「编译器与 CPU 的重排」正是它要约束的对象。

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

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l3-core/lesson-036-compiler`](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-036-compiler)。clone 仓库后进入 `examples/l3-core`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="验证一次边界检查消除" task="对 sum 函数生成 ssa.html，找出执行 BCE 的那个 pass，再用 go tool compile -S 确认汇编里没有 panicIndex；随后把循环改成下标可能越界的写法，对比结果。" :hints='["GOSSAFUNC=main.sum go build bce.go 会生成 ssa.html","grep panicIndex 检查边界检查","改成 i 的上界与 len 无关的写法会重新引入检查"]' solution="GOSSAFUNC=main.sum go build bce.go\ngo tool compile -S bce.go | grep panicIndex\n原写法：无 panicIndex（BCE 生效）\n越界写法：出现 CALL runtime.panicIndex" explain="编译器只有在能静态证明下标合法时才会消除边界检查，证明依赖 SSA 上的范围分析。" />

## 随堂检测

<Quiz no="036-1" q="想看某个函数被优化器逐 pass 改写的全过程，应该用哪条命令？" :options='["go vet","GOSSAFUNC=main.f go build","go build -gcflags=-m","go tool nm"]' answer="1" explain="GOSSAFUNC 指定函数后，编译器把各轮 SSA pass 的 IR 写进 ssa.html，可逐步对比优化前后。" />
<Predict no="036-2" q="一个小函数因为含 select 和 defer 而没被内联，最可能的原因是什么？" :options='["函数返回了指针","内联成本超预算或命中禁止内联的构造","它没有 import","目标平台不支持"]' answer="1" output="内联被拒绝，函数保留独立栈帧并产生 CALL" explain="内联预算约 80 单位，select、defer、go、recover 等构造成本高或直接禁止内联。" />

## 一页纸速记

<KnowledgeCard :points='["流程：语法与类型检查 → SSA → 优化 pass → 汇编与 .o → 链接","SSA 每变量只赋一次、用 Phi 合并，便于常量传播与 BCE","内联预算约 80 单位，内联还能放大逃逸分析与边界检查消除的效果","-gcflags=-m 读逃逸与内联，objdump / nm / GOSSAFUNC 做验证"]' :pitfalls='["只凭源码猜测优化是否发生，不做命令验证","为内联揉乱代码，或随处加 go:noinline 干扰优化器"]' summary="编译产物与源码并不一致，优化结论要用 -m、ssa.html 与反汇编逐条核实。" />

## 小结与下一讲

- SSA 让各类优化有了统一的载体，`GOSSAFUNC` 能把它可视化。
- 内联是「小函数免费、还能带动其它优化」的关键，但受预算与构造限制。
- 逃逸、内联、边界检查都能用命令验证，别凭感觉调优。

编译器也留了一道绕过类型系统的后门，代价与边界正是最后一讲。[下一讲：unsafe、cgo 与跨边界成本](/core/unsafe-cgo)
