---
title: 第 59 讲 · 性能剖析：pprof / trace / 火焰图
description: 建立可复现的性能诊断流程，掌握 CPU / Mem / Block / Mutex profile、火焰图读法、trace 与线上安全采集
---

<StageHeader stage="L5 · 工程化与质量" level="★★★★☆" time="50 分钟" goal="建立可复现的性能诊断流程" prereq="第 58 讲 · 设计模式与 Functional Options / wire" />

# 第 59 讲 · 性能剖析：pprof / trace / 火焰图

> 「接口变慢了」是一句没法行动的结论。可行动的是：CPU 时间花在哪个函数、谁在疯狂分配、哪些 goroutine 卡在锁上。pprof 与 trace 把运行时状态变成可查的证据，但工具本身不产生判断——这一讲的重点是**读图**和**安全采集**：知道宽平顶代表什么，也知道线上哪些开关不能常开。

## 这一讲你会得到什么
<CheckList id="goal-59" title="本讲目标" :items='["采集 CPU / Heap / Block / Mutex 四类 profile，并说清各自回答什么问题","读懂火焰图的横轴、纵轴与宽平顶，定位自身耗时的函数","用 trace 看调度延迟，并掌握线上安全采集的边界"]' />

## 1. 先建立直觉

把 profile 想成给运行中的程序拍 X 光：CPU profile 是每隔一小段时间问一句「现在在跑谁」，问得多了，谁出现得多谁就宽；heap profile 是拍一张内存快照，看谁占着对象不放；block 与 mutex profile 是看谁在排队等锁。火焰图把这些堆栈叠起来，越宽的横条越值得先动刀。

<ConceptDiagram type="perf-loop" title="优化闭环：基线 → 定位 → 改动 → 验证" />

关键心态是：**没有基线的优化只是猜测**。先跑一次 Benchmark 或采一份 profile，改且只改一处，再用 benchstat 或第二份 profile 证明差异。凭感觉换实现，经常越改越慢。

## 2. 语法与规则

第一块代码手工采集一份 CPU profile，适合离线复现一段确定的热点。

```go
// 手动采集 CPU profile：适合离线复现一段热点代码
package main

import (
	"fmt"
	"os"
	"runtime/pprof"
	"strings"
)

// hot 故意制造一段 CPU 热点，便于在 profile 里看到
func hot(n int) int {
	s := ""
	for i := 0; i < n; i++ {
		s += "x"
	}
	return len(s) + strings.Count(s, "x")
}

func main() {
	f, err := os.Create("cpu.pprof")
	if err != nil {
		fmt.Println("创建 profile 文件失败:", err)
		return
	}
	defer f.Close()
	if err := pprof.StartCPUProfile(f); err != nil {
		fmt.Println("启动 CPU profile 失败:", err)
		return
	}
	defer pprof.StopCPUProfile()

	fmt.Println("热点结果:", hot(20000))
	fmt.Println("已写出 cpu.pprof，可用 go tool pprof 分析")
}
```

<OutputBox title="运行结果" output="热点结果: 40000\n已写出 cpu.pprof，可用 go tool pprof 分析" note="StartCPUProfile 与 StopCPUProfile 必须成对，defer 保证异常路径也能落盘。" />

第二块展示线上最常用的接入方式：副作用导入 `net/http/pprof` 会注册一组 `/debug/pprof/` 端点。生产上要把它挂在内网独立端口，而不是业务端口。

```go
// 引入 net/http/pprof 后会注册诊断端点，务必只挂内网端口
package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof" // 副作用导入：把处理器注册到 DefaultServeMux
	"net/http/httptest"
	"strings"
)

func main() {
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(rec, req)
	fmt.Println("诊断端点状态码:", rec.Code)
	fmt.Println("是否列出 heap:", strings.Contains(rec.Body.String(), "heap"))
}
```

```text
诊断端点状态码: 200
是否列出 heap: true
```

第三块说明 block 与 mutex 采样默认关闭，必须按需打开、用完复位；heap profile 则可直接写出。

```go
// 内存、阻塞与锁竞争 profile：各有开关，别全量常开
package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
)

func writeHeap() error {
	f, err := os.Create("heap.pprof")
	if err != nil {
		return err
	}
	defer f.Close()
	return pprof.WriteHeapProfile(f)
}

func main() {
	// 阻塞与互斥锁采样默认关闭，按需打开，结束后复位
	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(1)
	defer runtime.SetBlockProfileRate(0)
	defer runtime.SetMutexProfileFraction(0)

	if err := writeHeap(); err != nil {
		fmt.Println("写 heap profile 失败:", err)
		return
	}
	fmt.Println("已写出 heap.pprof")
	fmt.Println("goroutine 至少为 1:", runtime.NumGoroutine() != 0)
}
```

```text
已写出 heap.pprof
goroutine 至少为 1: true
```

<LineByLine title="逐行看 CPU profile 采集" code="f, err := os.Create(cpu.pprof)\ndefer f.Close()\nif err := pprof.StartCPUProfile(f); err != nil {\n    return\n}\ndefer pprof.StopCPUProfile()\n_ = hot(20000)" :notes='["创建输出文件，profile 是二进制采样数据","确保文件句柄被关闭","开始按固定频率采样当前调用栈","启动失败就直接返回，不再继续","if 条件闭合","defer 保证采样在所有路径上都被停止并写出","真正被测的热点代码放在起止之间"]' />

### 四类 profile 与「按需开关」为什么这样设计

采样方式不是随意选的，它由「开销」和「要回答的问题」共同决定。

CPU profile 用采样而不是逐函数计时，是因为给每次调用加插桩的开销大到会改变被测程序本身；Go 的 profiler 靠定时信号按固定频率中断、记录当前调用栈，谁被采到的次数多谁就占 CPU 多。这决定了它是**统计估计**：短时采样噪声大，所以要采满几十秒、并在同一负载下多轮对比。heap profile 分 `-alloc_space` 与 `-inuse_space`，是因为「谁在疯狂分配」和「谁还占着内存不放」是两件事：一个反复分配又立刻释放的临时对象，前者很高、后者很低，优化它未必降低常驻内存，反而可能白忙。

block 与 mutex 默认关闭，是因为它们的采样要在每次阻塞、每次加锁的路径上记账，全量常开会明显拖慢服务；所以 `SetBlockProfileRate(1)` 与 `SetMutexProfileFraction(1)` 是显式开关，用完必须复位为 0。trace 独立于 pprof，则因为 pprof 给的是「聚合后的统计」，看不到时间维度——goroutine 在可运行队列里等了多久、一次 GC 停顿了多久、某段时间 CPU 为何空闲，只有事件流能回答，代价是文件更大、开销更高，只适合短时定点采集。最后，`net/http/pprof` 靠副作用导入在 `init()` 里把端点注册到 `DefaultServeMux`，接入最省事，但也意味着业务若正好用默认 mux 且对外暴露，诊断端点会一起暴露——这就是必须另挂内网独立端口的由来。

| 手段 | 回答什么 | 开销 | 主要代价 |
| --- | --- | --- | --- |
| 打点计时 / 日志埋点 | 自己埋的那几处耗时 | 低 | 覆盖不全，改一次埋一次 |
| CPU profile（采样） | 哪些函数占 CPU | 约百分之几 | 统计估计，需足够时长 |
| heap profile | 分配来源与存活对象 | 抓取触发一次 GC | 两种口径容易看混 |
| block / mutex | 等待与锁竞争 | 常开明显拖慢 | 必须按需开关并复位 |
| trace | 调度延迟、GC 停顿 | 高 | 文件大，只能短时采 |

### 可验证实验：读一次 top 输出

第一块代码写出 `cpu.pprof` 后，用下面的命令看最热的几个节点：

```bash
go tool pprof -top -nodecount=5 cpu.pprof
```

```text
File: app
Type: cpu
Time: Nov 12, 2026 at 10:00 (CST)
Duration: 30s, Total samples = 2.10s (7.00%)
Showing nodes accounting for 1.90s, 90.48% of 2.10s total
      flat  flat%   sum%        cum   cum%
     1.20s 57.14% 57.14%      1.20s 57.14%  main.hot
     0.40s 19.05% 76.19%      0.70s 33.33%  runtime.mallocgc
```

逐字段看：`Type: cpu` 是采样类型；`Duration: 30s` 是采集窗口，`Total samples = 2.10s (7.00%)` 表示这段时间里真正采到的 CPU 时间只有 2.10 秒、占比 7%，说明程序大部分时间在等而不是算——这时去优化 CPU 函数就是找错方向，该去看 block 或 trace。`flat` 是该函数自身（不含它调用的子函数）消耗的 CPU 时间，`flat%` 是占比，`sum%` 是自上而下累加；`cum` 是该函数连同所有下游调用的累计耗时，`cum%` 是占比。定位规则很实用：`flat` 高且 `cum` 与 `flat` 接近，说明时间花在它自己身上，优先动它（示例里的 `main.hot`）；`flat` 低但 `cum` 高，说明时间在下游，该顺着调用栈往下看，而不是动外层。`mallocgc` 那行 flat 19% 却 cum 33%，正提示分配链路还有下游值得追。

## 3. 深入一步

四类 profile 回答的问题互不重叠，选错工具等于白采：

| profile | 采集方式 | 回答什么问题 |
| --- | --- | --- |
| cpu | StartCPUProfile 或 /debug/pprof/profile | CPU 时间花在哪个函数 |
| heap | WriteHeapProfile 或 /debug/pprof/heap | 谁在分配、谁还活着 |
| block | SetBlockProfileRate 或 /debug/pprof/block | 谁在等 channel、等系统调用 |
| mutex | SetMutexProfileFraction 或 /debug/pprof/mutex | 锁竞争热点在哪 |
| trace | runtime/trace 的 Start | 调度延迟、GC 停顿、goroutine 状态 |

采集之前还要想清楚目标：是要降低延迟，还是要省内存，两者往往互相牵制。目标写成可测的数字，例如把某接口的 P99 从 80 毫秒压到 50 毫秒，profile 才知道该往哪个方向读；否则一份报告里 CPU、分配、锁都有热点，反而无从下手。

<Term word="火焰图" text="把采样到的调用栈按层级叠成的图，横轴宽度表示该函数在采样中出现的占比，纵轴表示调用栈深度。" />

<Callout type="warn" title="易错点：火焰图怎么读" text="横轴是采样占比而不是时间顺序，颜色只是区分函数不代表冷热。要找的是「宽平顶」：某个函数自己占很宽且上方没有更宽的子栈，说明时间花在它自身；若宽条上是层层子调用，该看 -cum 累计值，别急着优化最外层。" />

### 四类 profile 的采样信号各来自哪个 runtime 钩子

「CPU profile 靠定时信号中断记录调用栈」可以再往下追一层：每类 profile 的信号来源其实是不同的 runtime 钩子，这决定了它们各自能证明什么。

`pprof.StartCPUProfile` 最终调用 `runtime.SetCPUProfileRate(hz)`（默认 100Hz），在 Unix 上由 `setitimer(ITIMER_PROF)` 周期性发出 `SIGPROF`，信号处理器 `runtime.sigprof`（`src/runtime/signal_unix.go`、`src/runtime/proc.go`）在中断瞬间抓取当前 goroutine 的调用栈写进 `profBuf`。所以它是 **OS 定时信号驱动的统计估计**，只采到「正在 CPU 上跑」的栈，天然看不到阻塞、睡眠、等锁这些 off-CPU 时间。heap profile 走 `mallocgc`（`src/runtime/malloc.go`）：分配时按 `MemProfileRate`（默认每 512KB 采样一次）记一条 `memRecord`，`WriteHeapProfile` 前再触发一次 GC 拿存活对象快照，因此同一份数据能给出 `alloc_objects / alloc_space`（累计分配）与 `inuse_objects / inuse_space`（GC 后存活）四种口径。block 与 mutex 则在阻塞/解锁点记账：`chansend`、`chanrecv`、`selectgo`、`semacquire` 调 `runtime.blockEvent`（`src/runtime/mblock.go`），`sync.Mutex.Unlock` 发现等待者时走 `mutexevent`（`src/runtime/mprof.go`），分别由 `SetBlockProfileRate` 与 `SetMutexProfileFraction` 控制开关。`runtime/trace` 是另一条路：在 gopark / goready、GC 各阶段、系统调用进出等调度点**埋事件**，属于事件驱动而非采样，所以能看到调度延迟与 GC 停顿的时间维度，代价是文件大、开销高。

从机制就能推出上一节那几条实践结论，且可证伪：CPU profile 的 `Total samples` 占比很低时，说明时间花在 off-CPU，去优化 CPU 函数就是找错方向，该看 block 或 trace；heap 抓取会触发一次 GC，线上频繁抓 heap 会影响延迟；block/mutex 默认关闭，是因为每次阻塞与解锁都要记账，全量常开明显拖慢服务；`runtime.MemProfileRate` 与 `GODEBUG=memprofilerate=1` 能调采样密度，但越密开销越大。

可复现实验（Go 1.23.8 实测）。`go tool pprof -raw` 会打印未聚合的原始头，直接把采样周期与口径列名暴露出来：

```bash
# 先用本讲第一块代码写出 cpu.pprof，第三块写出 heap.pprof
go tool pprof -raw cpu.pprof  | head -8
go tool pprof -raw heap.pprof | head -8
```

```text
# cpu.pprof
PeriodType: cpu nanoseconds
Period: 10000000
Time: 2026-11-12 14:54:00 +0800 CST
Duration: 1.90
Samples:
samples/count cpu/nanoseconds
         44  440000000: 1 2 3 4 5 6 7 8

# heap.pprof
PeriodType: space bytes
Period: 524288
Samples:
alloc_objects/count alloc_space/bytes inuse_objects/count inuse_space/bytes
```

逐字段看：cpu 的 `PeriodType: cpu nanoseconds` 与 `Period: 10000000` 表示采样周期是 1000 万纳秒，即 10ms / 100Hz，正是 `SetCPUProfileRate(100)` 的信号间隔；`Samples` 下 `samples/count cpu/nanoseconds` 两列，第一条 `44 440000000` 是「这个调用栈被采到 44 次」与「按 10ms 折算的 4.4 亿纳秒 CPU 时间」，后面一串数字是栈帧的 location 编号。heap 的 `PeriodType: space bytes` 与 `Period: 524288` 表示每分配 512KB 采样一次，正是 `MemProfileRate` 的默认值；四个列名一次性列出累计分配对象数/字节数与当前存活对象数/字节数，证明这四种口径同源、都来自 `mallocgc` 的采样记录。上一节 `-top` 看的 `flat / cum`，就是对这些原始样本按函数聚合后的结果。

**边界与失效条件**：CPU profile 是统计估计，短时采样噪声大、且采不到 off-CPU 段，不能据此断言「某函数一定慢」；`Period` 可调但提高频率会放大中断开销；heap 的 `inuse_*` 依赖抓取那一刻的一次 GC，是快照而非峰值，反复分配又立刻释放的对象 `-alloc_space` 高、`-inuse_space` 低；block / mutex 是记账加采样，不是精确计数，`rate=1` 仍有遗漏，且默认关闭时必须显式打开、用完复位；trace 事件流开销最高，只能短时定点采。这些机制共同决定了：profile 只能说明「采样窗口里发生了什么」，跨负载、跨机器、跨 Go 版本都不可直接比较。

**版本前提**：本实验在 Go 1.23.8（darwin/arm64）实测；`go tool pprof -raw` 的头部格式、`SetCPUProfileRate` 默认 100Hz、`MemProfileRate` 默认 512KB 长期稳定，但 `GODEBUG` 默认值会随版本变化（Go 1.20 起这些默认值被写进二进制的 `DefaultGODEBUG`，第 60 讲 `go version -m` 能看到）。第 32 讲展开分配器，第 95 讲展开调度器源码。

### trace.out 里到底存了什么：header、事件表与 `go tool trace` 的视图

前面把 trace 归为「事件驱动的调度时间线」，却没打开过文件本身。理解二进制格式，才知道为什么它比 pprof 大、为什么换 Go 版本可能读不了、以及 `go tool trace` 那些视图是从哪几类事件算出来的。

`src/internal/trace/version/version.go` 里 `headerFmt = "go 1.%d trace\x00\x00\x00"` 定义了文件头，`ReadHeader` 用 `fmt.Fscanf` 从开头读出版本号；`Version` 常量列着 `Go111=11`、`Go119=19`、`Go121=21`、`Go122=22`、`Go123=23`，`Current = Go123`。注释特别说明：Go 1.22 引入了**不向后兼容的新事件格式**，1.23 又加了事件，但「1.22 的 trace 永远是合法的 1.23 trace」。所以文件头不只是个魔数，它是解析器选择事件表的依据——1.21 及更早用 `oldtrace.go` 的老解析器，1.22 起用 `go122.Specs()` 这张新表。

实测（Go 1.23.8，darwin/arm64；用 `runtime/trace.Start` 写一段含一个 goroutine 与 channel 收发的程序）并看头部字节：

```bash
xxd -l 96 /tmp/l5v3/trace.out
```

```text
00000000: 676f 2031 2e32 3320 7472 6163 6500 0000  go 1.23 trace...
00000010: 0101 ffff ffff ffff ffff ff01 c8ee a8cf  ................
00000020: be09 8580 8080 8080 8080 8000 08a8 d6b9  ................
00000030: 0701 0180 e080 de16 e8ec a8cf be09 8480  ...
```

逐字段：前 16 字节 `67 6f 20 31 2e 32 33 20 74 72 61 63 65 00 00 00` 正是 ASCII 的 `go 1.23 trace` 加三个 `\x00`——与 `headerFmt` 逐字对应，`23` 就是 `Version` 里的 `Go123`；`\x00\x00\x00` 是补齐的尾部，让后续数据从 16 字节对齐处开始。第 17 字节起（偏移 0x10）不再是文本，而是变长编码的事件流：`01` 一类的小整数是事件类型码，`ff...` 与 `80 80 80...` 是 LEB128 / uvarint 形式的参数（时间戳、ID、长度等），所以同一段字节里既有 ASCII 也有紧凑二进制。整个文件 2310 字节，却只是一次 goroutine 创建 + channel 收发的极短程序——**trace 记录的是事件流而非聚合样本，开销与文件体积都远大于 CPU profile**，这正是它只能短时定点采（前面「按需开关」表）的格式层原因。

事件表在 `src/internal/trace/event/go122/event.go`，`go122.Specs()` 按类型码顺序列出，每条注释都带参数清单：

| 类别 | 代表事件（1.22 起） | 参数（摘自源码注释） |
| --- | --- | --- |
| 结构 | `EvEventBatch` | generation、M ID、timestamp、batch length |
| 表 | `EvStacks` / `EvStack` / `EvStrings` / `EvString` | 栈表项 `PC/函数字符串 ID/文件字符串 ID/行号`，字典项 `ID/长度/字符串` |
| 采样 | `EvCPUSamples` / `EvCPUSample` | timestamp、M ID、P ID、goroutine ID、stack ID |
| P | `EvProcsChange` / `EvProcStart` / `EvProcSteal` | timestamp、P ID、P seq… |
| G | `EvGoCreate` / `EvGoStart` / `EvGoStop` / `EvGoBlock` / `EvGoUnblock` | timestamp、goroutine ID、seq、stack ID |
| GC/STW | `EvGCBegin` / `EvGCEnd` / `EvSTWBegin` / `EvHeapGoal` | timestamp、seq、heap alloc/goal |
| 1.23 新增 | `EvGoSwitch` / `EvGoSwitchDestroy` / `EvGoStatusStack` | goroutine 切换、带栈的状态 |

逐字段读表能推出三件事：其一，事件里到处是 `stack ID`、`string ID` 而不是完整字符串 / 栈文本——字符串与栈各自集中在 `EvStrings`/`EvStacks` 段里按 ID 引用，这是压缩手段，也是为什么 `go tool trace` 要先把整份文件解析成「字符串表 + 栈表 + 事件流」才能出图；其二，`EvEventBatch` 带 `M ID` 与 `batch length`，说明事件按「每个 M 一批」分块写入（对应 `src/internal/trace/batch.go`），多核并发写 trace 时各自写自己的 batch，解析器再按 generation 排序归并（`order.go`）——这是「事件驱动」能低开销记录的实现基础；其三，`EvGoStart`/`EvGoStop`/`EvGoBlock` 这类带 `timestamp` 与 `goroutine ID` 的事件，正是 `go tool trace` 把 goroutine 生命周期画成时间线的原料。

`go tool trace` 的各个视图因此不是另采了一份数据，而是**对同一份事件流的不同聚合**：Goroutine analysis 按 `EvGoCreate`/`EvGoStart`/`EvGoDestroy` 统计每个 goroutine 在每种状态（可运行、运行中、阻塞）各待了多久；Network / Sync / Syscall blocking 分别按 `EvGoBlock` 的 reason 分类汇总阻塞时长；Scheduler latency 从「goready 到 gostart」的时间差算等待调度的时间；Minimum mutator utilization 从 `EvSTWBegin`/`EvSTWEnd` 与 GC 事件算 GC 对应用线程的占用；`EvCPUSample` 还能被导出成 pprof（`cmd/trace/pprof.go`），这就是 `-pprof` 之类子命令的来源。可证伪的一条：把 trace 文件头里的 `go 1.23` 改成 `go 1.99`，`go tool trace` 会立刻报「版本不受支持」而不是尝试解析——事件表是按版本号选的，格式不认识就无法解码后面的字节。这也解释了为什么跨 Go 版本的 trace 不该当稳定制品归档。

**边界与失效条件**：trace 是精确事件记录而非采样，但「精确」指事件不丢，不代表业务因果完整——用户态 `trace.Log` 才写 `EvUserLog`，没埋点的业务阶段看不到；`EvCPUSample` 仍来自信号采样，所以 trace 里的 CPU 样本与 pprof 一样是统计估计；文件体积随 goroutine 数与事件频率线性增长，长时采集会迅速膨胀到数十 MB；1.22 前后格式不兼容，旧版 `go tool trace` 读不了新 trace。

**版本前提**：本机 Go 1.23.8（darwin/arm64）实测，`headerFmt` / `Version` / `go122` 事件表取自该版本源码；Go 1.22 起启用新 trace 格式（`Go122`），1.23 新增 `EvGoSwitch` 等事件但沿用 `go122.Specs()`；`xxd` 的字节序与地址随 GOARCH / 程序内容变化，头部 16 字节的文本部分是稳定的。第 95 讲展开调度器源码，前面几节讲 pprof 的四类采样钩子，这里补的是「trace 这份文件自身的格式」与「视图如何从事件算出来」；第 54 讲的 fuzz 覆盖率计数器是另一套插桩，别与这里的事件混淆。

### `GODEBUG=memprofilerate` 改的到底是什么：从采样分母到记录数

前面提到 `runtime.MemProfileRate` 与 `GODEBUG=memprofilerate` 能调 heap 采样密度，但没验证「调了会怎样」。把这条从源码追到可代入的数字，才谈得上「线上安全采集」里的取舍。

`src/runtime/malloc.go` 的 `nextSample()`（约 1462 行）注释写得很直白：目标是「平均每隔 `MemProfileRate` 字节采一次，但在分配时间线上完全随机」，这对应**泊松过程**，两次采样之间的距离服从 `exp(MemProfileRate)` 指数分布，于是 `nextSample` 返回 `fastexprand(MemProfileRate)`；`MemProfileRate == 1` 时有快路径直接返回 0（1470 行「rate is 1 时 next_sample 不使用」），即每次分配都记录。`src/runtime/extern.go`（约 141 行）则写明 `GODEBUG=memprofilerate=X` 会在启动时把这个值写进 `runtime.MemProfileRate`。默认值 512KB（前面 `pprof -raw` 实验里的 `Period: 524288`）就是这么来的。

实测（Go 1.23.8，darwin/arm64；程序分配 20000 个大小在 64–71 字节间变化的对象，调 `runtime.MemProfile(nil, true)` 读取当前记录数）：

```go
package main

import (
	"fmt"
	"runtime"
)

var sink []*[]byte

func main() {
	for i := 0; i < 20000; i++ {
		b := make([]byte, 64+i%8)
		sink = append(sink, &b)
	}
	n, ok := runtime.MemProfile(nil, true)
	fmt.Printf("MemProfileRate=%d  MemProfile 记录数 n=%d ok=%v\n", runtime.MemProfileRate, n, ok)
}
```

```bash
go run memprof.go                              # 默认
GODEBUG=memprofilerate=1 go run memprof.go     # 每次分配都采
GODEBUG=memprofilerate=0 go run memprof.go     # 关闭采样
```

```text
MemProfileRate=524288  MemProfile 记录数 n=4 ok=false   （默认，n 随采样在 1~5 间波动）
MemProfileRate=1        MemProfile 记录数 n=102 ok=false （每次分配都采，按栈与大小类聚合后稳定）
MemProfileRate=0        MemProfile 记录数 n=0  ok=true   （关闭采样）
```

逐字段：默认 `MemProfileRate=524288`（512KB），程序总分配约 `20000 × 68 ≈ 1.3 MB`，按指数分布期望命中 `1.3MB / 512KB ≈ 2.6` 次采样，归并到「调用栈 × 大小类」后实测 `n` 在 1~5 间波动（采样是随机的，这里取一次实跑的 4）；`memprofilerate=1` 时每次分配都采，但 `MemProfile` 按「分配点栈 + 大小类」聚合（本例只有 8 种大小 × 少数几个调用栈），所以 `n=102` 而不是 20000——**记录数是去重后的桶数，不是分配次数**，这正是 heap profile 能压缩存储的原因；`memprofilerate=0` 时 `nextSample` 分支被跳过、`n=0`、`ok=true`（`ok` 表示「传入的切片够不够装下全部记录」，传 nil 时 `ok=false` 只说明「没给缓冲」，不代表出错）。

公式与代入：`MemProfileRate = R` 时，采样概率 `p = 1/R`（R=0 关闭），总分配字节数 `B` 下的期望采样次数 `E ≈ B / R`。代入默认 `B ≈ 1.3×10⁶`、`R = 524288` → `E ≈ 2.6`；`R = 1` → `E ≈ B`（每次都采）。这与实测的 4 条 / 102 桶方向一致（桶数 ≤ 采样次数，且受聚合影响）。可证伪结论：`GODEBUG=memprofilerate=1` 并不会让 `MemProfile` 记录数等于分配次数，因为记录按栈与大小类聚合；把对象大小改成全相同（单一大小类），`R=1` 时的 `n` 会明显小于 102——采样是逐次发生的，存储是聚合的。

由此推出线上采集的边界，每条都能反证：`memprofilerate=1` 让每次 `mallocgc` 都走 profiler 记账，开销随分配频率线性增长，**只适合短时离线复现，不能常开**（呼应前面「heap 抓取触发一次 GC、别高频抓」）；默认 512KB 是「采样精度 ↔ 运行时开销」的折中，把它调小能提高小对象泄漏的定位精度，代价是 CPU 与 profile 体积；`ok=false` 常被误读为「profile 有错」，实际只是 `nil` 切片的正常返回——要拿记录得传足容量的 `[]runtime.MemProfileRecord` 并看返回的 `n`。可证伪对照：把 `runtime.MemProfile(nil, true)` 换成传入 `make([]runtime.MemProfileRecord, 200)`，`ok` 会变 true，但 `n` 不变——`ok` 只反映缓冲够不够，与采样率无关。

**边界与失效条件**：heap profile 的 `inuse_*` 依赖抓取那一刻的一次 GC，是快照而非峰值；`MemProfile` 记录的是「采样到的分配点」，小对象若始终没被采中就不会出现在 profile 里，所以「profile 里没有某函数」不能证明它没分配；`GODEBUG` 是进程级、启动时读取的，运行中改 `runtime.MemProfileRate` 影响的是之后的新分配；`DefaultGODEBUG` 把默认值写进二进制（第二版提过），所以不同 Go 版本即便不设 GODEBUG，采样率也可能不同，跨版本比较 heap profile 前要确认这一点。

**版本前提**：本机 Go 1.23.8（darwin/arm64）实测；`MemProfileRate` 默认 512KB、`nextSample` 的泊松 / 指数分布实现长期稳定，`GODEBUG=memprofilerate` 自早期版本提供，`DefaultGODEBUG` 自 Go 1.20 起把默认值写入构建信息（第 60 讲）。第 32 讲展开分配器，本讲前面讲四类 profile 的 runtime 钩子与 `pprof -raw` 的 Period，这两节补的是「trace 文件的二进制格式与视图来源」和「memprofilerate 改采样分母的可量化效果」；eBPF / perf 这类 OS 层采样是 pprof / trace 之外的第三条路径，本讲未展开。

### 三条最常见的误解

**误解一：火焰图横轴是时间，越靠右越晚发生。** 反例：把它当执行时序读，得出「最后调用的函数最慢」的结论，照着去改一通毫无收益。横轴是采样占比（同层函数按名字排序堆叠），颜色只用于区分函数、不代表冷热，纵轴才是调用栈深度。正确读法是找宽平顶——某个函数自己很宽且上方没有更宽的子栈，或配合 `-cum` 看累计值。

**误解二：pprof 里某个函数宽，就说明它写得烂、该改它。** 反例：`runtime.mallocgc` 占宽条时，根因往往是上层反复构造临时对象，而不是 GC 本身；此时去调 `GOGC` 只是把问题往后推，常驻内存与延迟都没解决。正确做法是沿调用栈往上找分配来源，回到第 54 讲的 `B/op` 与 `allocs/op` 交叉验证。

**误解三：诊断端点方便，挂到业务端口一起暴露省事。** 反例：`/debug/pprof/` 一旦对外可达，任何人都能触发长时 CPU 采样或反复抓 heap，造成 CPU 与内存放大，响应体里还可能带出内部路径与堆栈。正确做法是只绑 `127.0.0.1` 的独立端口、加访问控制、不进 Ingress 与 Service，采完确认 block / mutex 开关已复位。

## 4. 工程实践

一组可以直接照抄的命令：

```bash
# CPU：交互式看最热函数，或直接出火焰图
go tool pprof cpu.pprof
(pprof) top
(pprof) list hot
go tool pprof -http=:8081 cpu.pprof

# 从运行中的服务在线采 30 秒 CPU
go tool pprof -http=:8081 http://127.0.0.1:6060/debug/pprof/profile?seconds=30

# 内存：分别看累计分配与当前存活
go tool pprof -alloc_space http://127.0.0.1:6060/debug/pprof/heap
go tool pprof -inuse_space http://127.0.0.1:6060/debug/pprof/heap

# 阻塞与锁竞争
go tool pprof http://127.0.0.1:6060/debug/pprof/block
go tool pprof http://127.0.0.1:6060/debug/pprof/mutex

# trace：看调度延迟、GC 停顿与 goroutine 生命周期
go tool trace -http=:8082 trace.out
```

读图时有两个高价值信号：若 `runtime.mallocgc` 或 GC 相关函数占了宽条，问题多半在分配而非算法，应回到 `B/op` 与 `allocs/op`；若锁与 channel 相关函数很宽，别急着加机器，先看是不是锁粒度太大。trace 则能补上 profile 看不到的时间维度——goroutine 在可运行队列里等了多久、一次 GC 停顿了多久。

还要留意可比性：不同负载、不同机器、不同 Go 版本采到的 profile 不能直接比。定位时应在同一压测流量下采基线与改动后各一份，用 `top` 与 `-cum` 交叉验证，再决定动哪里；否则很容易把流量波动误读成优化收益。

<Compare title="凭感觉与看证据" :panels='[{"label":"写法 A · 凭感觉","code":"// 反例：没有证据就改\n// 觉得 strings.Builder 慢，换成加号\n// 上线后 P99 反而升高","note":"结论不可复现，也没有回归防线"},{"label":"写法 B · 看证据","code":"// 正例：先定位再动手\ngo tool pprof -http=:8081 cpu.pprof\n// 只改 top 里的宽平顶函数\n// 改完用 benchstat 验证","note":"一次只动一处，差异有数据支撑"}]' />

线上安全采集是硬边界：CPU 采样有约百分之几的开销，heap 抓取会触发一次 GC，block 与 mutex 全量常开明显拖慢服务，因此默认关闭、按需短时打开并立刻复位。端点必须只监听 `127.0.0.1` 的独立诊断端口，加访问控制，绝不随业务端口对外暴露。

也别把 profile 当成一次性动作：优化合入后，应把基线与结论写进提交信息或性能文档，并在回归门禁里保留同一组基准。这样下次有人改回旧写法时能立刻发现，经验才不会随人员流动而丢失。

<Callout type="tip" title="一条可执行的检查" text="把诊断端口写进部署清单：只绑 127.0.0.1、不进 Ingress、不进 Service；每次线上采集记录起止时间与采样率，采完确认开关已复位。" />

### 一次真实故障：把 P99 从 800 毫秒定位到一行字符串拼接

现象：某接口 P99 从 80 毫秒涨到 800 毫秒，CPU 与内存监控都没有明显异常，也没有泄漏迹象。排查路径：

1. 先排除外部因素：QPS 平稳、下游依赖延迟不变，把范围锁定在本服务；
2. 在预发用同一压测流量采 30 秒 CPU profile，保证基线与改动后可比；
3. `go tool pprof -top` 显示 `runtime.mallocgc` 与字符串相关调用累计占比偏高，用 `-cum` 顺藤摸到一个格式化函数；
4. `list` 逐行看，发现新提交的一段循环里用 `+=` 拼日志字符串，单次请求要构造上万次临时字符串；
5. 回看 `go test -bench -benchmem`，`allocs/op` 从 3 涨到约 12000，印证问题在分配而不在算法；
6. 改成预分配容量的 `strings.Builder` 后复采一份 profile，P99 回到 90 毫秒，再用 benchstat 证明差异显著（呼应第 54 讲）。

这条路径之所以有效，是因为每一步都排除了一种误判：先排除流量与下游，避免把外部波动当成本服务退化；在同一负载下采两份，避免把机器差异当成优化收益；用 `flat` 与 `cum` 区分「自己耗」与「下游耗」，避免对着 GC 本身动刀；最后用分配指标交叉验证，才敢下结论。替代做法与代价也清楚——靠日志埋点猜只能看到自己埋的那几处，直接加机器成本上升且问题没解决，凭感觉换实现甚至可能更慢。

### 把性能回归接进 CI

```yaml
# .github/workflows/perf.yml（节选：性能回归门禁）
- uses: actions/setup-go@v5
  with:
    go-version-file: go.mod
- name: 基准回归
  run: |
    go test -bench=. -benchmem -count=6 ./... | tee new.txt
    go tool benchstat bench/baseline.txt new.txt
- name: 诊断端口不对外
  run: grep -q 'PPROF_ADDR=127.0.0.1:6060' deploy/prod.env
```

版本锁定是可比性的前提：`go-version-file: go.mod` 让 CI 用仓库声明的 Go 版本，因为编译优化、`strings.Builder` 的实现、GC 行为都会随版本变，不同版本采到的 profile 与基准数字不能直接比较；基线文件要随运行环境更新，benchstat 也建议用 Go 1.24 起的 `tool` 指令把版本写进 `go.mod`。`PPROF_ADDR` 这类配置进版本库并被 CI 校验，正是第 56 讲配置外置与第 60 讲流水线的落点：诊断端口只绑内网这条硬边界，不该靠部署时的人工记忆。

### 与 L10 实战工程结构的呼应

L10 里这套闭环反复出现：`/projects/kv-3-async` 的 WAL 追加与锁分片、`/projects/net-shortlink` 的热 key、`/projects/micro-seckill` 的秒杀热点，都要先靠 pprof 定位再动手；第 100 讲「从 pprof 到 30w QPS」就是它的完整版。再往前，第 54 讲的 benchstat 负责证明收益，第 55 讲的 `trace_id` 让一次慢请求能被还原，第 60 讲把基准排进流水线防止回归——优化结论只有落成基准与文档，才不会随人员流动而丢失。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l5-engineering/lesson-059-profiling`](https://github.com/lfire/go/tree/main/examples/l5-engineering/lesson-059-profiling)。clone 仓库后进入 `examples/l5-engineering`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="写出 heap profile" task="补全一个函数：创建指定路径的文件，把当前堆 profile 写进去，并确保文件被关闭。" :hints='["用 os.Create 创建文件","pprof.WriteHeapProfile 接收 io.Writer","用 defer 关闭句柄"]' solution="func writeHeap(path string) error {\n    f, err := os.Create(path)\n    if err != nil {\n        return err\n    }\n    defer f.Close()\n    return pprof.WriteHeapProfile(f)\n}" explain="WriteHeapProfile 接受任意 io.Writer，写文件时务必 defer Close，避免采样数据没落盘。" />

## 随堂检测

<Quiz no="59-1" q="火焰图里某个函数的横条越宽，通常代表什么？" :options='["它被调用的次数最少","它在采样中出现的占比越高，通常越耗 CPU","它的代码行数最多","它一定发生了内存泄漏"]' answer="1" explain="横轴是采样占比而非时间；宽说明该函数或其调用栈频繁出现在采样里，通常对应 CPU 热点。" />

<Predict no="59-2" q="代码里没有调用 runtime.SetBlockProfileRate，直接访问 /debug/pprof/block 会看到什么？" :options='["完整的阻塞采样数据","几乎没有数据的空 profile","编译报错","自动打开全量采样"]' answer="1" output="（几乎没有数据的空 profile）" explain="block 采样默认关闭，必须显式设置采样率才会记录，这是为了避免线上常开带来额外开销。" />

## 一页纸速记

<KnowledgeCard :points='["CPU 看耗时、Heap 看分配、Block 看等待、Mutex 看锁竞争","火焰图横轴是采样占比，优先盯宽平顶函数","-alloc_space 看累计分配，-inuse_space 看当前存活","trace 补上调度延迟与 GC 停顿的时间维度"]' :pitfalls='["没有基线就凭感觉优化，改完不做对比","把横轴当时间顺序、把颜色当冷热","线上常开 block / mutex 采样，或把诊断端口对外暴露"]' summary="可复现的诊断闭环是：先有基线，再用 profile 定位，一次只改一处，最后用数据验证。" />

## 小结与下一讲

- 四类 profile 各管一段，`go tool pprof` 与 `-http` 火焰图是主要读图手段。
- 横轴是采样占比，宽平顶才是自身耗时的函数；`-cum` 区分自己耗与被调函数耗。
- 线上采集要短时、按需、只绑内网，采完复位开关。

[下一讲：CI/CD、Makefile、代码生成与发布](/engineering/cicd)
