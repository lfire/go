---
title: 第 94 讲 · runtime 源码：进程启动与初始化
description: 沿 rt0_go、schedinit、newproc 到 mstart 的主流程，读懂 main 之前运行时完成的全部初始化
---

<StageHeader stage="L9 · 资深进阶与源码" level="★★★★★" time="55 分钟" goal="读懂 rt0_go 到 main 的路径" prereq="第 35 讲 · 程序启动、init 顺序与包初始化" />

# 第 94 讲 · runtime 源码：进程启动与初始化

> 用户写的 main 从来不是进程执行的第一行代码。操作系统把控制权交给入口汇编之后，运行时要先搭好栈、堆、处理器与垃圾回收，再把 main.main 包装成一个 goroutine 交给调度器。读懂这段路径，才能解释「为什么 main 里 NumGoroutine 至少是 1」「为什么命令行参数一进 main 就已经可读」「为什么把重活塞进 init 会拖慢启动」。本讲以本机 Go 1.23.8 的 GOROOT 源码为准，每一步都配一段可运行程序去验证。

## 这一讲你会得到什么
<CheckList id="L9-94" title="本讲目标" :items='["说出从 rt0_go 到 mstart 的四个关键阶段", "用 runtime.Stack 验证 main 就是 1 号 goroutine", "用 inittrace 与 ReadMemStats 观察初始化顺序与分配器状态"]' />

## 1. 先建立直觉
把进程启动想成剧院开演：观众只看见主角登台，也就是用户写的 main，但在此之前舞台、灯光、节目单与后台调度表必须全部就位。Go 的 runtime 正是这套后台。入口汇编先设置好 g0 栈、参数与 CPU 特性，schedinit 建立 P 的个数与内存系统，newproc 把用户入口登记成第一个 G，最后 mstart 才让调度循环转起来。因此 Go 程序从第一刻起就是「被调度」出来的，而不是被直接调用的。读源码时先抓主流程与关键函数签名，不要陷入每一条汇编指令。
<ConceptDiagram type="runtime-startup" title="进程启动的四段主流程" caption="依据 Go 1.23.8：入口汇编、运行时初始化、main G 入队、调度启动" />

## 2. 语法与规则
下面这些位置都以 Go 1.23.8 为准。行号会随版本漂移，但函数名与结构体字段基本稳定，记调用关系比记行号可靠得多。

- `src/runtime/asm_amd64.s:159`：`TEXT runtime·rt0_go(SB),NOSPLIT|NOFRAME|TOPFRAME,$0`，架构相关的入口。
- `src/runtime/proc.go:782`：`func schedinit()`，设置最大栈、调用 mallocinit、初始化 GC 与按核数建立 P。
- `src/runtime/proc.go:147`：`func main()`，运行时的 main，负责 doInit、gcenable 与提前启动 sysmon。
- `src/runtime/proc.go:4975`：`func newproc(fn *funcval)`，把函数包装成一个新的 G。
- `src/runtime/proc.go:1754`：`func mstart()`，M 绑定 P 后进入调度循环。
- `src/runtime/runtime2.go` 的 `type g struct`（422 行）、`type p struct`（650 行）、`type schedt struct`（776 行）。

三段程序分别验证 goroutine 与 P、初始化次序、以及分配器状态。每段都是完整可编译的独立程序。

```go
package main

import (
	"fmt"
	"runtime"
)

// 观察一：进入 main 时，运行时已把用户入口包装成 1 号 goroutine，
// 并按 CPU 核数建立好 P。这些正是 schedinit 的成果。
func main() {
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, false) // false 表示只取当前 goroutine 的栈
	fmt.Print(string(buf[:n]))

	fmt.Println("goroutine 数 =", runtime.NumGoroutine())
	fmt.Println("P 数量（GOMAXPROCS）=", runtime.GOMAXPROCS(0))
	fmt.Println("CPU 逻辑核数 =", runtime.NumCPU())
}
```

```go
package main

import (
	"fmt"
	"os"
)

// 观察二：包级变量、init、main 依次执行，命令行参数在 main 之前已就绪。
var stage = record("包级变量初始化")

func record(s string) string {
	fmt.Println(s)
	return s
}

func init() {
	fmt.Println("init 函数")
}

func main() {
	fmt.Println("main 函数")
	fmt.Println("参数个数 =", len(os.Args))
	fmt.Println("首个参数（可执行文件路径）=", os.Args[0])
}
```

```go
package main

import (
	"fmt"
	"runtime"
)

// 观察三：main 刚执行时，堆分配器与 GC 已被 schedinit / gcenable 初始化。
func main() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Println("堆已向 OS 申请的字节数 =", m.HeapSys)
	fmt.Println("已完成 GC 次数 =", m.NumGC)
	fmt.Println("分配器已初始化 =", m.HeapSys > 0)
}
```

<OutputBox title="三段程序的输出示例（核数因机器而异）" output="goroutine 1 [running]:\nmain.main()\n    tmp/main.go:12 +0x...\ngoroutine 数 = 1\nP 数量（GOMAXPROCS）= 8\nCPU 逻辑核数 = 8\n---\n包级变量初始化\ninit 函数\nmain 函数\n参数个数 = 1\n---\n堆已向 OS 申请的字节数 = 3670016\n已完成 GC 次数 = 0\n分配器已初始化 = true" note="HeapSys 的具体数值随平台变化，但它一定大于 0，说明 mallocinit 已经跑过；NumGC 为 0 则说明还没触发过回收。" />

<LineByLine title="rt0_go 之后的三步关键调用" code="CALL runtime·schedinit(SB)\nCALL runtime·newproc(SB)\nCALL runtime·mstart(SB)" :notes='["调用 schedinit：设定最大栈、初始化内存分配器与 GC，并按 GOMAXPROCS 建立 P 的列表", "调用 newproc：把用户包里的 main.main 封装成第一个 G，放入当前 P 的运行队列", "调用 mstart：M 绑定 P 之后进入 schedule 循环，从此由调度器挑选 G 执行"]' />

## 3. 深入一步
启动不是一条直线，而是「汇编准备、运行时自举、用户初始化、调度接管」四段。关键分界在 `runtime.main` 里：`doInit(runtime_inittasks)` 按依赖顺序执行全部包的 init，`gcenable()` 让 GC 开始工作，随后 `newproc` 创建的 main G 才会被 mstart 拉起。sysmon 也是在这里通过 `newm(sysmon, nil, -1)` 作为独立 M 提前启动的。理解这一点，就能明白为什么 init 阶段开出的后台 goroutine 虽然能入队，却不该依赖「所有全局变量都已初始化」这个假设。

<Term word="g0 与 m0" text="每个 M 都有一个 g0 栈用于调度与栈管理；m0 是进程主线程，g0 是它上面那个不跑用户代码的调度 goroutine。用户 G 永远运行在自己的栈上。" />

| 阶段 | 关键函数（Go 1.23.8） | 完成了什么 | 可验证的观察手段 |
| --- | --- | --- | --- |
| 入口 | `rt0_go`（asm_amd64.s） | 设置 g0 栈、argc 与 argv、CPU 特性检测 | `os.Args` 在 main 中可读 |
| 自举 | `schedinit`（proc.go） | 最大栈、mallocinit、mcache、P、GC 参数 | `GOMAXPROCS(0)` 等于 `NumCPU()`，`HeapSys` 大于 0 |
| 初始化 | `runtime.main` 的 `doInit` | 按依赖图跑全部 init，随后 `gcenable` | `GODEBUG=inittrace=1` 打印每包耗时 |
| 接管 | `newproc` 与 `mstart` | 建立 main G 并进入 schedule 循环 | `runtime.Stack` 显示 goroutine 1 是 `main.main` |

<Callout type="warn" title="别背行号，也别在 init 里放飞" text="行号每个版本都变，记调用关系更可靠。init 阶段 main G 还没被调度，后台任务依赖的全局状态可能未就绪；重活放进 init 还会阻塞启动且难观测。" />

### 源码精读与可复现验证（以 Go 1.21~1.23 为准，版本可能变化）

读启动源码建议按「一条汇编、一个自举函数、一个结构体」的顺序对照 GOROOT。第一步看 `src/runtime/asm_amd64.s` 的 `TEXT runtime·rt0_go(SB)`：它把 `argc`、`argv` 从栈搬到全局变量，给 `m0` 与它的 `g0` 设好栈边界，做 CPU 特性检测，再依次 `CALL runtime·schedinit(SB)`、把 `runtime·mainPC(SB)` 交给 `newproc`、最后 `CALL runtime·mstart(SB)`。arm64 的入口在 `asm_arm64.s`，这里只以 amd64 为例。第二步看 `src/runtime/proc.go` 的 `func schedinit()`：它不创建任何用户 G，只铺地基——`mallocinit` 初始化堆与 `mcache`，`mcommoninit` 初始化当前 M，`gcinit` 准备 GC 参数，`procresize(procs)` 按 `GOMAXPROCS` 分配包级 `allp []*p` 并把空闲 P 串进 `sched.pidle`。函数签名多年未变，但内部调用清单每版都可能增删，读的时候盯住 `procresize` 一个即可。

第三步看 `src/runtime/runtime2.go`：用户 main 被包装成 G 后，执行现场存在 `g.sched` 这个 `gobuf` 里，字段 `sp`（栈顶）、`pc`（下一条指令地址）、`g`（回溯指针）由 `newproc` 经 `gostartcallfn` 填好；`mstart` 起来后 `schedule` 选中这个 G，`gogo(&g.sched)` 才把 `pc` 装进 CPU 跳进 `runtime.main`。`runtime.main` 内部再跑 `doInit(runtime_inittasks)`、`gcenable()`，并用 `newm(sysmon, nil, -1)` 提前拉起监控线程，最后才调用 `main.main`。一个可复现的观察实验：先用 `go tool nm` 记下 `runtime.main` 的地址，再 `go tool objdump -s runtime.schedinit ./startup` 看函数体前几条 `CALL` 是否指向 `mallocinit` 或 `procresize`；然后把默认环境与 `GOMAXPROCS=1` 各跑一次 `GODEBUG=inittrace=1`，对比各包 init 的 `clock` 列，会发现 P 的个数影响并发 init 的调度，却不改变「包级变量 → init → main」的相对次序。

结论的适用边界要写清：上述函数名与字段以 Go 1.21~1.23 的 linux/amd64、darwin/arm64 为准，行号与内部调用每版都可能变；`rt0_go` 是汇编入口，Windows 与 plan9 走另一套 `rt0` 文件；`inittrace` 只统计包 init，不含更早期的 `mallocinit`、`procresize` 耗时，不能当完整启动时长，量全貌得配合 `runtime/trace` 或外部计时；带 cgo 的二进制还会在这些步骤之前先跑 C 运行时初始化，顺序与纯 Go 不同。

把上面的符号与账单坐实，两条命令都不用改代码：

```bash
go build -o startup ./cmd/startup
GODEBUG=inittrace=1 ./startup 2> init.log
go tool nm ./startup | grep -E 'rt0_go|runtime.main|main.main|schedinit'
```

```text
init github.com/me/startup/internal/config @0.15 ms, 0.21 ms clock, 120 bytes, 3 allocs
init net @1.02 ms, 0.44 ms clock, 4184 bytes, 13 allocs
init main @1.61 ms, 0.03 ms clock, 208 bytes, 2 allocs
  401000 T runtime.rt0_go
  43a2c0 T runtime.schedinit
  43af40 T runtime.main
  44dd80 T main.main
```

`inittrace` 每行四段数字各有含义：`@0.15 ms` 是进程启动到该包 init 开始的时刻；`0.21 ms clock` 是这段 init 自己消耗的 CPU 时间；`120 bytes` 与 `3 allocs` 是期间累计的堆分配字节数与分配次数。判断哪个包最贵要看 `clock` 与 `bytes` 两列，`@` 靠后只说明排得晚，不代表跑得久。`go tool nm` 的三列是符号地址、`T`（代码段里的全局函数）与符号全名；`runtime.rt0_go` 地址最低，因为它是入口汇编，`runtime.main` 是运行时的 main，`main.main` 才是用户入口。用 `go tool objdump` 反汇编 `rt0_go`，能对上那几条 `CALL` 确实依次指向 `schedinit`、`newproc`、`mstart`。

<SequenceDiagram title="从 rt0_go 到用户 main 的调用链" caption="以 Go 1.21~1.23 amd64 为准：入口汇编完成自举后，由 mstart 把 main G 调度起来" :actors='["rt0_go","schedinit","newproc","mstart","runtime.main","main.main"]' :steps='[{"from":"rt0_go","to":"schedinit","label":"调用 schedinit","note":"设 g0 栈与参数","kind":"call"},{"from":"schedinit","to":"schedinit","label":"procresize 建 P","note":"mallocinit 与 allp","kind":"self"},{"from":"schedinit","to":"rt0_go","label":"返回自举结果","note":"运行时地基就绪","kind":"return"},{"from":"rt0_go","to":"newproc","label":"newproc 建主 G","note":"取 runtime.mainPC","kind":"call"},{"from":"newproc","to":"rt0_go","label":"返回并入队","note":"主 G 进 runq","kind":"return"},{"from":"rt0_go","to":"mstart","label":"调用 mstart","note":"M 绑定 P","kind":"call"},{"from":"mstart","to":"runtime.main","label":"schedule 选中","note":"gogo 读 g.sched","kind":"call"},{"from":"runtime.main","to":"runtime.main","label":"跑 init 与 GC","note":"newm 起 sysmon","kind":"self"},{"from":"runtime.main","to":"main.main","label":"调用户 main","note":"参数已就绪","kind":"call"},{"from":"main.main","to":"runtime.main","label":"返回并退出","note":"执行 exit hook","kind":"return"}]' />

这张图把第 2 节的三条 `CALL` 展开成了完整的责任交接：自举（`schedinit`）与主 G 创建（`newproc`）都在 `runtime.main` 执行之前完成，因此用户 main 一登场就已经是被调度出来的 G，而不是被直接调用的函数。

| 结构（runtime2.go / proc.go） | 关键字段 | 启动期写入者 | 字段含义 |
| --- | --- | --- | --- |
| `gobuf`（即 `g.sched`） | `sp` / `pc` / `g` | `newproc` 经 `gostartcallfn` | main G 首次被调度时的栈顶、入口指令地址与回溯 G |
| `m`（m0） | `g0` / `curg` / `p` | `rt0_go`、`mstart` | 调度用栈、当前用户 G、绑定的 P |
| `p` | `runq` / `runnext` / `status` | `procresize` | 本地队列、优先 G 与 `_Pidle` 等状态位 |
| `schedt`（即 `sched`） | `runq` / `goidgen` / `nmspinning` | `schedinit`、`newproc` | 全局队列、G 的 id 分配器、自旋 M 计数 |
| 包级 `allp` | `[]*p` | `procresize` | 按 `GOMAXPROCS` 建立的全部 P，供 sysmon 与窃取遍历 |

把上表和第 2 节的可观测数字对上：`GOMAXPROCS(0)` 读的就是 `allp` 的长度，`runtime.Stack` 能打印栈正是 `g.sched.sp` 与 `pc` 被填好的结果，而 `NumGoroutine` 的最小值为 1，对应 `newproc` 只创建了一个用户 G。

<Callout type="tip" title="源码阅读建议" text="先在 GOROOT 里搜 runtime.mainPC 定位入口符号，再顺 schedinit、procresize、newproc、mstart 四个函数读一遍，最后回 runtime2.go 对字段；不要一上来逐行啃 asm_amd64.s，先记调用关系，行号留给编辑器帮你找。" />

### 常见误读与反例

启动阶段的误读大多来自「把用户视角当成机器视角」。下面三条都能用第 2 节的程序或本节命令直接推翻。

| 误读 | 为什么站不住（源码推导） | 正确写法或反例 |
| --- | --- | --- |
| 「`main.main` 是进程执行的第一行代码，运行时是进 main 之后才启动的」 | `rt0_go` 依次 `CALL schedinit`、把 `runtime.mainPC` 交给 `newproc`、`CALL mstart`；`runtime.main` 里的 `doInit` 与 `gcenable` 都跑在 `main.main` 之前。用户 main 是 `gogo(&g.sched)` 调度出来的第一个用户 G，而非被直接调用的函数 | 在 main 第一行用 `runtime.Stack` 打印，栈底是 `main.main()` 且 `NumGoroutine()` 最小为 1；`os.Args` 已可读，因为 argc/argv 在入口汇编里就从栈搬到了全局变量 |
| 「包 init 按文件名字母序或书写顺序执行，所以能靠 init 之间的先后依赖全局变量」 | `doInit(runtime_inittasks)` 按编译期生成的**包导入依赖图**拓扑排序，同一包内多个 init 才按源文件顺序；跨包只看谁 import 谁，与字母序无关。且 init 阶段 main G 尚未被调度，此时开出的后台 G 不该假设所有全局状态都已就绪 | 让 A 显式 import B，把共享状态的初始化放进 B 的 init 或 `main` 显式编排；用 `GODEBUG=inittrace=1` 的 `@` 列观察真实先后，而不是猜文件名 |
| 「`inittrace` 里 `@` 时刻靠后的包最慢，把各段 clock 加起来就是完整启动耗时」 | 每行四段里 `clock` 才是该包 init 自己消耗的 CPU 时间，`@` 只是开始时刻，`bytes` 与 `allocs` 是期间累计分配。更要紧的是 `inittrace` 只统计**包 init**，`rt0_go`、`schedinit`、`mallocinit`、`procresize` 以及 cgo 的 C 运行时初始化都不在它的账单里 | 定位最贵包看 `clock` 与 `bytes` 两列；量完整启动用 `runtime/trace` 或进程外计时，两者结论不可互相替代 |

<Quiz q="一个最小程序在 main 第一行读 os.Args 就已经有值，这些参数是谁、在什么时候放好的？" :options='["编译器在链接时写进数据段","入口汇编 rt0_go 把 argc 与 argv 从栈搬到全局变量","os 包第一次被调用时向内核查询","newproc 建 main G 时顺带填充"]' answer="1" explain="rt0_go 在调用 schedinit 之前就完成 argc/argv 搬迁与 g0 栈设置，所以参数在 main 之前就绪，与 os 包无关。" />

### 观测手段的能证与不能证

第 3 节列了五种观察手段，这里补上各自的证据边界，避免「看到数字就下结论」。

| 手段 | 能证明什么 | 不能证明什么 |
| --- | --- | --- |
| `GODEBUG=inittrace=1` | 各包 init 的开始时刻、自身 CPU 耗时与期间分配量 | 完整启动时长（不含自举与 cgo）、哪个包拖慢了首个请求；`@` 靠后不等于跑得久 |
| `go tool nm` | 二进制里 `rt0_go`、`runtime.main`、`main.main` 的地址与 `T` 类型，入口汇编地址最低 | 运行期的调用顺序与控制流；静态符号表不含谁先执行 |
| `go tool objdump` | `rt0_go` 函数体里确实依次 `CALL schedinit`、`newproc`、`mstart` | 这些调用在目标机器上一定产生了可观察效果；它也不替代 GODEBUG 的动态证据 |
| `runtime.Stack` / `NumGoroutine` | main 是被调度的 1 号 G、其栈帧已建立 | G 的精确创建时刻，也不能区分系统 G 与用户 G 的调度先后 |

把上表与第 3 节的结构字段表对读会更清楚：`nm` 与 `objdump` 给的是「静态存在性」证据，`inittrace` 与 `Stack` 给的是「动态时序」证据，任何一条单独都不够，交叉印证才能把「main 之前发生了什么」坐实。这也解释了为什么第 4 节要用 `inittrace` 查 init 耗时、却不能拿它当整个进程的启动时长。

### 用 go tool objdump 坐实启动调用链

`go tool nm` 只能看到符号**在不在**，真正要确认「谁先调谁」得反汇编入口。下面这组命令不需要改代码，直接对第 4 节构建出的 `startup` 二进制操作：

```bash
go build -o startup ./cmd/startup
go tool objdump -s 'runtime.rt0_go' ./startup | grep -E 'CALL|TEXT' | head -20
```

```text
TEXT runtime.rt0_go(SB) /usr/local/go/src/runtime/asm_amd64.s
  asm_amd64.s:159   0x401000   MOVQ 0(SP), DI
  asm_amd64.s:161   0x401008   MOVQ DI, runtime.argc(SB)
  asm_amd64.s:180   0x401050   CALL runtime.schedinit(SB)
  asm_amd64.s:195   0x4010a2   CALL runtime.newproc(SB)
  asm_amd64.s:204   0x4010c0   CALL runtime.mstart(SB)
```

第一行 `TEXT ... /usr/local/go/src/runtime/asm_amd64.s` 是符号名与源文件路径，说明这段来自入口汇编而非 Go 代码；之后每行三段依次是 `文件:行号`、指令地址（如 `0x401050`）与指令本身，`CALL runtime.schedinit(SB)` 里的 `SB` 是静态基址寄存器，表示调用全局符号。两条 `MOVQ` 把栈顶的 `argc` 搬进全局变量 `runtime.argc`，对应第 2 段程序里 `os.Args` 一进 main 就可读；三条 `CALL` 地址递增、顺序与第 2 节 `LineByLine` 完全一致，正是「先自举、再建主 G、最后接管调度」的可数证据。要注意寄存器名、行号与地址随版本和平台漂移，arm64 要看 `asm_arm64.s`，Windows 与 plan9 走另一套 `rt0` 文件——这个实验只证明 amd64 上入口汇编的调用顺序，不证明它在别的架构上同样成立。

### 与 L10 源码级实战的呼应

- KV 贯穿线 ③ [WAL、持久化与并发控制](/projects/kv-3-async)：崩溃恢复要在服务启动时重放 WAL、重建索引，这段逻辑必须放在 `main` 里显式编排而不是 `init`——否则失败无法返回错误、也没法加超时，正是本讲「init 使用边界」的落地；恢复慢时还能用 `inittrace` 区分是自己代码慢还是依赖包慢。
- [实战 08 · 分布式任务调度系统](/projects/concurrent-scheduler)：Master 选主成功后要先加载任务模型与 cron 表达式、再启动执行器，顺序错了会漏跑一轮；这用的就是「按依赖图排序 + main 显式编排」的思路，而不是把加载塞进各自包的 init。
- [实战 11 · Kubernetes Operator](/projects/cloud-operator)：控制器在 `Reconcile` 循环开始前要注册 CRD、建立 informer 缓存并等缓存同步完成，本质是把 `runtime.main` 的「先初始化、再接管」搬进应用自己的启动序列；Operator 启动慢时，本讲的 `inittrace` 与 `objdump` 两类证据同样适用。

## 4. 工程实践
排查「服务启动慢」时，第一件事不是靠打点猜，而是让运行时自己报账。`GODEBUG=inittrace=1` 会在每个包 init 结束时打印耗时与内存分配，直接指出哪个包最贵；配合 `go build -gcflags=-m` 还能看到全局变量是否被放到了堆上。生产环境建议把这项输出接到启动日志里做长期对比。

```bash
GODEBUG=inittrace=1 ./myserver 2> init.log
```

<Compare title="初始化工作放在哪里" :panels='[{"label":"写法 A：塞进 init","code":"func init() {\n    mustLoadConfig()\n    mustWarmCache()\n}","note":"隐式执行、无法传参、失败只能 panic，阻塞启动且难以拆分耗时"},{"label":"写法 B：main 显式编排","code":"func main() {\n    cfg := loadConfig()\n    warmCache(cfg)\n    go serve(cfg)\n    wait()\n}","note":"顺序可见、可加超时与错误返回、可写单测，除 inittrace 之外还有业务日志可查"}]' />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l9-advanced/lesson-094-runtime-startup`](https://github.com/lfire/go/tree/main/examples/l9-advanced/lesson-094-runtime-startup)。clone 仓库后进入 `examples/l9-advanced`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="验证 main 的 goroutine 身份" task="写程序用 runtime.Stack 抓当前 G 的栈，打印 NumGoroutine，判断 GOMAXPROCS 是否等于 NumCPU。" :hints='["分配 64KB 缓冲区传给 runtime.Stack", "用返回长度判断栈是否非空"]' solution="buf := make([]byte, 65536)\nn := runtime.Stack(buf, false)\nfmt.Println(n != 0)\nfmt.Println(runtime.NumGoroutine())\nfmt.Println(runtime.GOMAXPROCS(0) == runtime.NumCPU())" explain="输出应为 true、1、true：依次说明栈存在、只建了 main G、P 数量取自 NumCPU。完整写法见第二段代码。" />

## 随堂检测
<Quiz no="94-1" q="用户包里的 main.main 是由谁包装成 goroutine 的？" :options='["链接器直接内联", "doInit 自动完成", "newproc 封装成 G 入队", "操作系统创建主线程时顺带创建"]' answer="2" explain="proc.go 在启动阶段调用 newproc 建立第一个 G，之后才由 mstart 进入调度循环。" />
<Predict no="94-2" q="一个没有 init 的最小程序，在 main 第一行调用 runtime.NumGoroutine() 会得到什么？" :options='["0","1","2","等于 GOMAXPROCS"]' answer="1" output="1" explain="main 自己就是 1 号 G；sysmon 是独立 M 上的系统 goroutine，不计入普通 G 数量。" />

## 一页纸速记
<KnowledgeCard :points='["启动四段：rt0_go、schedinit、newproc、mstart", "进入 main 时 P、堆分配器与 GC 已就绪", "init 先于 main，参数在 main 之前就绪"]' :pitfalls='["死记行号：版本升级即失效", "把重活塞进 init：阻塞启动", "以为 main 被直接调用"]' summary="Go 程序从第一刻起就在调度器里，main 只是第一个用户 G。" />

## 小结与下一讲
1. 启动分四段：入口汇编准备栈与参数，schedinit 建立运行时，doInit 跑完包初始化，mstart 接管调度。
2. main 是 newproc 创建的 1 号 G，P 的数量来自 NumCPU，堆分配器在进入 main 前已就绪。
3. 用 `GODEBUG=inittrace=1`、`runtime.Stack` 与 `ReadMemStats` 三个手段即可把启动过程从猜测变成可观测事实。

[下一讲：调度器源码：schedule / findRunnable / sysmon](/advanced/runtime-scheduler)
