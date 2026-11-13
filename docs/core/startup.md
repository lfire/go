---
title: 第 035 讲 · 程序启动、init 顺序与包初始化
description: 搞清 main 之前发生了什么：依赖图、init 执行顺序、runtime 初始化与 inittrace 启动耗时分析
---

<StageHeader stage="L3 · 核心机制" level="★★★★☆" time="40 分钟" goal="搞清楚 main 之前发生了什么" prereq="第 034 讲 · defer 实现与 open-coded 优化" />

# 第 035 讲 · 程序启动、init 顺序与包初始化

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

runtime 侧的入口是汇编 `rt0_go`，`schedinit` 里把 P 的数量设为 GOMAXPROCS 并初始化堆、GC 与栈池，随后 `newproc` 造出 main goroutine，`mstart` 启动调度，`runtime.main` 再调用 `doInit` 跑 `..inittask` 依赖图，最后转 `main.main`。这条路径的源码精读留到第 094 讲，本讲只需建立「main 之前有一段确定次序」的认知。链接器生成的 `..inittask` 符号可用 `go tool nm ./binary | grep inittask` 查到。

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

### 推导链：从 ..inittask 符号推到初始化顺序

<Callout type="note" title="进阶选读 · 首次学习可跳过" text="本段沿运行时/编译器源码做推导，属于加深理解的可选层。第一次学建议先掌握本讲主线与可运行结论，遇到尚未讲过的源码术语不必停下，学完对应讲次再回看，不影响后续理解。" />

结论落在链接器与运行时的交界：`cmd/link` 为每个包生成一个 `..inittask` 只读符号，`cmd/compile` 把该包的包级变量求值与所有 `init` 函数收进这个任务；`runtime/symtab.go` 的 `moduledata.inittasks` 保存全部任务的指针，`runtime/proc.go` 的 `runtime.main` 调用 `doInit` 按 `..inittask` 的依赖图递归遍历。三条推导链：

- **跨包顺序**：`..inittask` 里记着本包依赖的其它包的任务指针，`doInit` 先递归完成依赖再执行自己，于是「被依赖者先」由依赖图拓扑序保证，与 import 的书写先后无关。
- **包内顺序**：编译器对包级变量做引用依赖分析排求值次序，再把所有 `init` 按「文件名字典序、同文件出现顺序」串进任务；所以 `var b = f(a)` 必然排在 `a` 之后，而两个 `init` 按出现顺序跑。
- **为什么 `init` 不能有参数、返回值或被引用**：这样初始化任务才能退化成一张无依赖的函数指针表，由 `doInit` 线性遍历；否则初始化顺序会依赖任意表达式求值，静态图无法表达，`import _` 的副作用也会失控。

用符号表把「链接期生成的任务」亲眼看到。把第 2 节的 `init_order.go` 编译成 `app`：

```bash
go build -o app init_order.go
go tool nm ./app | grep -E 'inittask|main\.init'
GODEBUG=inittrace=1 ./app
```

<OutputBox title="go tool nm 与 inittrace 输出（Go 1.23 / arm64 实测，地址随构建变化）" output="  10015c410 D errors..inittask\n  10015c820 D go:main.inittasks\n  10015c520 D main..inittask\n  100099850 T main.init.0\n  1000998e0 T main.init.1\ninit main @0.85 ms, 0.024 ms clock, 2104 bytes, 5 allocs" note="D 是数据段（部分平台为 R 只读段），T 是代码；inittrace 一行给出时刻、耗时与分配量。" />

逐字段解释：`nm` 三列是地址、符号类型、符号名，`D` 表示数据段（部分平台为 `R` 只读段），`..inittask` 是链接器生成的任务描述表，`go:main.inittasks` 是把各包任务拼起来的列表；`T` 表示 `main.init.0`/`main.init.1` 是代码，两个编号正对应源码里两个 `init` 的出现顺序；`errors..inittask` 排在 `main..inittask` 前面，就是 import 依赖图拓扑序的直接证据。`inittrace` 那一行里 `init main` 是包名，`@0.85 ms` 是从进程启动到该包 init 开始的时刻，`0.024 ms clock` 是该包包级变量求值加所有 init 的耗时，`2104 bytes` 与 `5 allocs` 是这段时间的堆分配量。

**为什么这样设计**：把初始化顺序在编译与链接期固化成 `..inittask` 依赖图，运行时只需线性遍历，不必反射扫描包、维护动态依赖或处理环；代价是包之间不能循环导入——编译期就因依赖图成环而报错。替代方案（显式 `Setup()`）把时机交给调用方、可返回错误、易测试，但要求每个入口都记得调用，注册表式驱动仍选 init 正是为了不漏。这也解释了为什么本讲反复强调「重活移出 init」：依赖图保证的是次序，不是耗时。

### inittask 的字节级布局与 doInit 递归计数

上一节用 `go tool nm` 确认了 `..inittask` 这个符号存在、用 `inittrace` 量了耗时，但没拆开符号本身。`runtime/proc.go` 的 `initTask` 结构前半是 16 字节头：`state uint32`（0 未跑 / 1 进行中 / 2 完成）、`ndeps uint32`、`nfns uint32` 加 4 字节填充；后面紧跟 `ndeps` 个依赖任务的 PC 与 `nfns` 个 init 函数的 PC，各占一个机器字。链接器把各包任务拼进 `go:main.inittasks`，`runtime.main` 调 `doInit` 遍历这张表，`doInit1` 先按 `state == 2` 跳过已完成的（防重复、防环）、递归跑完 deps，再 `for i := 0; i < t.nfns; i++` 逐个调用 init 的 PC，最后置 `state = 2`。分支条件有两个：`nfns == 0` 时整段调用循环跳过，`state == 1` 则说明出现了本不该有的环。可观察输出因此有三处：`nm -size` 里 `..inittask` 的字节数、`main.init.N` 符号个数、`inittrace` 的行数与 `@时刻`。

用「只有 init 个数不同」的两个二进制把这三层量出来：

```go
// init3.go：3 个 init
package main

import "fmt"

var sink int

func init() { sink++ }
func init() { sink++ }
func init() { sink++ }

func main() { fmt.Println(sink) }
```

```go
// init1.go：只留 1 个 init，其余完全相同
package main

import "fmt"

var sink int

func init() { sink++ }

func main() { fmt.Println(sink) }
```

```bash
go build -o init1 init1.go && go build -o init3 init3.go
go tool nm -size ./init1 | grep -E 'main\.\.inittask| T main.init'
go tool nm -size ./init3 | grep -E 'main\.\.inittask| T main.init'
echo "--- 依赖图任务符号数 ---"
go tool nm ./init3 | grep -c inittask
echo "--- inittrace 逐包行数 ---"
GODEBUG=inittrace=1 ./init1 2>&1 | grep -c '^init '
GODEBUG=inittrace=1 ./init3 2>&1 | grep -c '^init '
```

<OutputBox title="nm -size 与计数实测（Go 1.23 / arm64，地址随构建变化）" output="--- init1 ---\n10015c410         16 D main..inittask\n100099750         32 T main.init.0\n--- init3 ---\n10015c520         64 D main..inittask\n100099750         32 T main.init.0\n100099770         32 T main.init.1\n100099790         32 T main.init.2\n--- 计数 ---\n任务符号数: 16 / 16\ninittrace 逐包行数: 13 / 13" note="D 是数据段、T 是代码段；第二列是符号字节数。任务符号数与逐包行数都不随 main 包内 init 个数变化。" />

逐字段解释：`nm -size` 三列是地址、字节数、符号类型加名字；`main..inittask` 从 16 字节涨到 64 字节（1 个 init 到 3 个 init），但**不是**严格的 `16 + 8×nfns`——链接器还要按对齐填充，2 个 init 时实测 32 字节、4 个 init 时仍是 64 字节，所以只能看趋势、不能当公式；`main.init.0/1/2` 每个是独立 TEXT 符号、实测各 32 字节，编号就是源码里的出现顺序。`go tool nm | grep -c inittask` 得到 16 个符号，其中含 `go:main.inittasks` 这类把各包任务拼起来的列表符号，真正被 `doInit` 逐包执行并打印 `inittrace` 的是 13 行；两个二进制这三项计数完全相同，说明它们由**依赖图里的包数**决定，与某个包内部写了几个 init 无关。

**反直觉结论的证伪**：「多写几个 init 只是多几次函数调用，不影响二进制；inittrace 也会因此多几行」两半都错。对照里每个 init 都生成一个 32 字节的独立 TEXT 符号并让 `..inittask` 变大，二进制确实变重；而 `inittrace` 逐包打印，1 个 init 与 3 个 init 都是 13 行，差别只落在 `init main` 那一行的 `clock`、`bytes`、`allocs` 上。由此也能推出为什么「重活移出 init」值得强调：依赖图保证的是**次序与调用次数**，init 数量增加的是代码体积与累计调用，不会让每个包多占一行诊断。

边界：`initTask` 字段布局与 `..inittask` 的对齐方式是 Go 1.23 的实现细节，跨版本先 `go version` 再对照 `runtime/proc.go`；`nm -size` 的字节数含链接器填充，别拿它反推 `nfns`；`inittrace` 的 `@时刻` 是从进程启动起的累计时刻、`clock` 只算本包；交叉编译改 `GOARCH`/`GOOS` 不改变依赖图顺序，但 32 位平台每个 PC 只占 4 字节，任务符号会更小；`init` 全在 main goroutine 上串行执行，包级变量初始化里用 `go` 启动的后台任务不受这层次序保护。

### `rt0_go` 到 `schedinit`：汇编入口的调用序列与 runtime 自身耗时

第 3 节的时间线把启动画成 `rt0_go → schedinit → newproc → doInit → main.main`，但没给出这条链在**汇编级**的真实指令，也没回答「`inittrace` 量不到的那一段 runtime 自身耗时怎么看」。这一节补这两处，依据以本机 Go 1.23.8 / darwin / arm64 为准。

**汇编入口的状态迁移**（`runtime/asm_arm64.s` 的 `rt0_go`）：先 `tlsinit` 装 TLS、`save_g` 把 g0 写进线程寄存器，`check` 校验运行时常量；随后 `MOVW 8(RSP), R0` 与 `MOVD 16(RSP), R0` 把 `argc/argv` 从内核栈复制到参数区，再依次 `args`、`osinit`、`schedinit`。接着把 `runtime·mainPC`（数据段里指向 `runtime.main<ABIInternal>` 的指针）压栈，调 `newproc` 造出 main goroutine，最后 `mstart` 切进调度循环；`mstart` 之后那几行 `MOVD $0, R0; MOVD R0, (R0)` 是「若居然返回就崩」的兜底。所以 `main.main` 之前真正由汇编保证的次序是：

```text
tlsinit → save_g → check → args → osinit → schedinit → newproc(mainPC) → mstart
         → runtime.main → doInit(..inittask) → main.main
```

`schedinit()`（`runtime/proc.go`）内部不是随意排列，注释把依赖写得很死：`raceinit` 必须最先（`mallocinit` 会调 `racemapshadow`）→ `worldStopped()` → `ticks.init()` → `moduledataverify()` → `stackinit()` → `mallocinit()` → `getGodebugEarly()` → `cpuinit(godebug)`（必须先于 `alginit`）→ `randinit()`（必须先于 `alginit`、`mcommoninit`）→ `alginit()`（此后才允许用 map/hash/rand）→ `mcommoninit(gp.m, -1)` → `modulesinit()` → `typelinksinit()` → `itabsinit()` → `stkobjinit()`（必须先于 GC 启动）→ `sigsave` → `goargs/goenvs/secure/checkfds/parsedebugvars` → `gcinit()` → `gcrash.stack = stackalloc(16384)` → `mProfStackInit(gp.m)` → 取 `ncpu` 或 `GOMAXPROCS` 环境变量 → `procresize(procs)` 建出 P → `worldStarted()`。字段级状态是：`sched.maxmcount = 10000`、`sched.lastpoll.Store(nanotime())`、`worldStopped` 到 `worldStarted` 之间整段时间**不允许 P 运行**。这也解释了为什么第 030 讲 `itabsinit` 能「在启动时把静态 itab 批量塞进表」——它排在 `alginit` 之后、GC 之前，正好在 runtime 可用而用户 init 未跑的窗口里。

**runtime 自身耗时**：`GODEBUG=inittrace=1` 的 `clock` 只覆盖「包级变量求值加所有 init」，不含 `schedinit` 这一大段。换一个观察角度——在**第一个 `init()` 里**读 `syscall.Getrusage(RUSAGE_SELF, &ru)`，`Utime`/`Stime` 是进程到目前为止消耗的 CPU 时间，`Minflt` 是次要缺页次数，`Maxrss` 是峰值常驻集；这些数恰好就是 `rt0_go` 到 `doInit` 之间 runtime 自身初始化的累计代价，且不需要 root 权限、不依赖 GODEBUG 解析。

新观察角度一：反汇编**链接后二进制的入口段**，把时间线对上真实 `CALL`。

```bash
go build -o app init_order.go       # 第 2 节那个程序即可
go tool objdump -s '^runtime\.rt0_go' ./app | grep -E 'CALL runtime\.(tlsinit|save_g|check|args|osinit|schedinit|newproc|mstart)'
sed -n '782,880p' "$(go env GOROOT)/src/runtime/proc.go"
sed -n '80,100p' "$(go env GOROOT)/src/runtime/asm_arm64.s"
```

<OutputBox title="rt0_go 入口调用链（Go 1.23.8 / darwin arm64）" output="  asm_arm64.s:27  CALL runtime.tlsinit.abi0(SB)\n  asm_arm64.s:59  CALL runtime.save_g.abi0(SB)\n  asm_arm64.s:74  CALL runtime.check.abi0(SB)\n  asm_arm64.s:84  CALL runtime.args.abi0(SB)\n  asm_arm64.s:85  CALL runtime.osinit.abi0(SB)\n  asm_arm64.s:86  CALL runtime.schedinit.abi0(SB)\n  asm_arm64.s:93  CALL runtime.newproc.abi0(SB)\n  asm_arm64.s:97  CALL runtime.mstart.abi0(SB)" note="八条 CALL 与源码行号一一对应；.abi0 后缀说明汇编入口走栈式 ABI（回看第 026 讲）。" />

逐字段解释：`tlsinit`/`save_g` 建立 g0 与 TLS，`check` 校验，`args` 收 `argc/argv`，`osinit` 取 `ncpu` 与页大小，`schedinit` 跑上面那一长串初始化，`newproc` 造 main goroutine，`mstart` 启动 M——顺序与 `sed -n '80,100p'` 的汇编原文完全一致。八条都带 `.abi0` 后缀，正是第 026 讲「汇编/反射/cgo 仍走栈式 ABI0」的现实落点：这段入口由汇编写成，调用的是 ABI0 入口而非寄存器 ABI 的 `ABIInternal`。`sed -n '782,880p' proc.go` 给出 `schedinit` 的调用顺序与「must run before」注释，把「为什么是这个次序」从时间线变成源码约束；`procresize(procs)` 与 `worldStarted()` 两行则把「P 何时可运行」钉在 `schedinit` 的末尾。

新观察角度二：用 `Getrusage` 量出 `inittrace` 看不到的那一段。目录中放两个带构建标签的文件：

```go
// rusage.go
//go:build !heavy

package main

import (
	"fmt"
	"syscall"
)

func init() {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	fmt.Printf("bare   Utime=%d.%06ds Stime=%d.%06ds Minflt=%d Maxrss=%d\n",
		ru.Utime.Sec, ru.Utime.Usec, ru.Stime.Sec, ru.Stime.Usec, ru.Minflt, ru.Maxrss)
}

func main() {}
```

```go
// heavy.go
//go:build heavy

package main

import (
	"fmt"
	_ "net/http"
	"syscall"
)

func init() {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	fmt.Printf("heavy  Utime=%d.%06ds Stime=%d.%06ds Minflt=%d Maxrss=%d\n",
		ru.Utime.Sec, ru.Utime.Usec, ru.Stime.Sec, ru.Stime.Usec, ru.Minflt, ru.Maxrss)
}

func main() {}
```

`go.mod` 写 `module startup` 与 `go 1.23`。

```bash
go run .
go run -tags heavy .
```

<OutputBox title="首个 init 时的 Getrusage（Go 1.23.8 / darwin arm64，数值随机型波动）" output="bare   Utime=0.001098s Stime=0.002225s Minflt=457 Maxrss=4128768\nheavy  Utime=0.003770s Stime=0.004375s Minflt=776 Maxrss=8945664" note="同一程序只多一个 net/http 空白导入，首个 init 之前累计的 CPU、缺页与常驻集都明显上升。" />

逐字段解释：`Utime.Sec/Usec` 与 `Stime.Sec/Usec` 是进程累计用户态/内核态 CPU 秒数，在**第一个 init 执行的瞬间**读，因此包含 `rt0_go` 到 `schedinit`、`doInit` 之前的全部 runtime 开销；`Minflt` 是次要缺页（映射到物理页但未换盘），`Maxrss` 是峰值常驻字节数。多一个 `_ "net/http"` 后四项同步上升：`Utime` 约从 1.1 ms 到 3.8 ms、`Minflt` 从 457 到 776、`Maxrss` 从约 4 MiB 到约 8.5 MiB——增量来自 `net/http` 及其依赖链的包级变量与 init，被算进了「首个 init 之前」的累计值，因为那些包的 init 排在 main 包 init 之前。这与第 4 节「重活移出 init」是同一现象，但观测手段从 `inittrace` 的逐包 `clock`/`bytes` 换成了进程级 CPU 与缺页计数：`inittrace` 只报 init 段，`Getrusage` 还能看到 runtime 自身那段。注意绝对值随机器与构建缓存波动，只比同一台机器上的相对增量。

边界：`rt0_go` 的指令序列是 arm64 + darwin 专属，amd64 或 Linux 的 `CALL` 目标与 `.abi0` 表现不同；`schedinit` 的调用顺序与各「must run before」注释是 Go 1.23 的约束，跨版本先对照 `runtime/proc.go`；`Getrusage` 的 `RUSAGE_SELF` 只算本进程、不含子进程，Windows 上字段语义不同；`Maxrss` 在 macOS 以字节计、Linux 以 KiB 计，跨平台别直接比数值；`init()` 里读 `Getrusage` 本身是一次系统调用，会给读数引入微秒级偏差；`go tool objdump` 的地址随构建变化，只比符号与源码行号。这条链正是第 094 讲（runtime 源码：进程启动与初始化）的入口，那里会把 `rt0_go` 到 `runtime.main` 逐帧精读；第 095 讲讲 `procresize` 如何确定 P 数量，第 096 讲讲 `gcinit` 与堆初始化的衔接。

### 边界、常见误解与前后联系

| 常见误解 | 正解 | 可验证依据 |
| --- | --- | --- |
| 包级变量按源码书写顺序初始化 | 按引用依赖分析，被引用者先；同一文件内也按依赖而非纯文本顺序 | 第 2 节 `b` 依赖 `a` 的输出 |
| import 的书写顺序决定 init 先后 | 由 import 依赖图拓扑序加文件名字典序决定 | `go tool nm` 里 `fmt..inittask` 排在 `main..inittask` 前 |
| inittrace 的 clock 就是程序启动总耗时 | 只覆盖该包 init 与包级变量求值，不含 runtime 自身初始化与 main 之后 | 对照 `inittrace` 与整体启动时间 |

边界要点：所有 init 都在 main goroutine 上**串行**执行，不存在并发初始化；包级变量初始化表达式里若用 `go` 启动后台任务，它可能在 init 完成前就跑、顺序不受保护；`inittrace=1` 的 `clock` 不含被依赖包的累计，`bytes`/`allocs` 是 init 期间的分配量而非常驻内存；`os.Exit` 与 init 里的 panic 会直接终止程序（main 还没开始，没有 recover 机会）；跨包无依赖的 init 顺序由文件名字典序决定、随文件名变化；交叉编译改变 GOARCH/GOOS 但不改变依赖图顺序；Go 版本升级可能改变 `inittask` 布局但语义不变；观测局限是 `inittrace` 看不到 runtime 自身初始化与链接、加载阶段的耗时，冷启动分析要叠加上一层计时或 `strace` 类工具。

前后联系：第 011 讲（包与 init 初识）给用法，本讲给次序与代价；第 094 讲（runtime 源码：进程启动与初始化）从汇编 `rt0_go` 到 `schedinit`、`newproc`、`mstart`、`runtime.main` 精读整条路径，是本讲最直接的源码延续，第 096 讲（GC 源码）讲 `schedinit` 里堆与 GC 的初始化，第 095 讲（调度器源码）讲 P 数量在 `schedinit` 中如何确定，第 100 讲（极致性能优化）把「把重活移出 init」列为冷启动优化项。

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

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l3-core/lesson-035-startup`](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-035-startup)。clone 仓库后进入 `examples/l3-core`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="找出最慢的 init" task="在一个导入了多个包的小程序上跑 GODEBUG=inittrace=1，找出 clock 值最高的包，并说明它的 bytes 与 allocs 意味着什么。" :hints='["inittrace 每行格式：包名 @时刻 ms, 耗时 ms clock, 字节, 分配次数","bytes 大说明 init 期分配多","把可疑的重活从 init 移到显式 Setup"]' solution="GODEBUG=inittrace=1 go run app.go\n观察输出中 clock 最大者：其 bytes 与 allocs 反映 init 期的堆分配；\n若某包字节数异常，检查它的包级变量与 init 是否做了填充或解码。" explain="inittrace 把「main 之前的隐藏成本」量化到包级别，是启动优化的第一手依据。" />

## 随堂检测

<Quiz no="035-1" q="两个互不依赖的包 A 和 B 都被 main 导入，它们的 init 先后由什么决定？" :options='["main 里 import 的书写顺序","依赖图拓扑序加文件名字典序，不应依赖隐含先后","按包名长度","随机"]' answer="1" explain="跨包按依赖图拓扑序、同包按文件名字典序与出现顺序；由于无依赖包之间顺序不稳定，不应写依赖它。" />
<Predict no="035-2" q="代码里写 import _ 某包，最可能发生什么？" :options='["编译报错，因为没有用到该包","只触发该包的 init，不引入任何标识符","把该包所有符号导入当前文件","跳过该包的初始化"]' answer="1" output="该包被正常初始化并执行其 init，当前文件不获得任何新标识符" explain="空白导入的唯一作用是触发 init，典型用途是让编解码器或数据库驱动完成自注册。" />

## 一页纸速记

<KnowledgeCard :points='["次序：跨包按 import 依赖图，包内先变量后 init，最后 main.main","runtime 侧 rt0_go → schedinit → newproc → doInit → main.main","init 适合无 I/O 的注册与校验，空白导入专为触发注册","GODEBUG=inittrace=1 量化每个包的 init 耗时与分配"]' :pitfalls='["在 init 里读配置、连库、启动后台 goroutine","依赖无依赖关系包之间不稳定的 init 先后"]' summary="main 之前是一段由依赖图确定的初始化流水线，重活应显式化而非塞进 init。" />

## 小结与下一讲

- 包级变量与 init 的先后由依赖关系决定，不是简单的书写顺序。
- init 只做轻量装配；全局大对象与重活会变成隐藏的启动成本。
- `GODEBUG=inittrace=1` 是定位启动慢包的第一手工具。

初始化跑完之后，机器真正执行的是编译器和链接器产出的指令。[下一讲：编译、链接、SSA 与内联](/core/compiler)
