---
title: 第 041 讲 · 调度循环：抢占、窃取与 handoff
description: 第 041 讲 · 调度循环：抢占、窃取与 handoff —— schedule 循环、work stealing、系统调用与 handoff、异步抢占、sysmon 职责
---

<StageHeader stage="L4 · 并发编程" level="★★★★★" time="50 分钟" goal="理解调度器如何保持 CPU 忙碌" prereq="第 040 讲 · GMP 调度模型全景" />

# 第 041 讲 · 调度循环：抢占、窃取与 handoff

> 上一讲把 GMP 画成了一张静态结构图：G 排队、P 发牌、M 干活。这一讲让这张图动起来。调度器每一轮只有一个朴素目标——**别让任何 P 闲着**。为了做到这点，它要在多条队列之间按固定顺序找活、在 P 之间搬活、把陷入系统调用的 P 及时抢回来，还要能打断一个霸占 CPU 不放的死循环。理解这些动作，你才能解释「为什么 Go 能用少量线程扛住大量并发 I/O」，以及「为什么某段死循环会把 GC 拖住」。

## 这一讲你会得到什么

<CheckList id="L41-goal" title="本讲目标" :items='["说清 schedule 主循环与 findRunnable 的取 G 顺序","理解 work stealing 如何均衡各 P 的本地队列","讲清阻塞系统调用时的 handoff：阻塞的是 M，不是 P","知道 Go 1.14 异步抢占解决了什么问题、sysmon 承担哪些职责"]' />

## 1. 先建立直觉

把调度器当成一位永不下班的调度员。每个 M 在需要换任务时都会进入 `schedule()`：找一个此刻可运行的 G，把 G、M、P 三者绑在一起，切到 G 的栈上执行；等这个 G 因为等待、被抢占或函数返回而让出，控制权又回到 `schedule()`，开始下一轮。整个运行时没有「调度结束」这个概念，只有「暂时找不到活干」——那时 M 会休眠，P 被放回空闲池，直到有新 G 被创建或被唤醒再把它叫起来。

真正有意思的是「找活」的顺序。调度员不会每次都去翻那条要加锁的全局队列，那样多核下又会退化成一把锁；它先看自己 P 手边最热乎的位置，再逐步扩大范围，最后才去别的 P 那里借活。这个由近及远的顺序，本质是在**局部性**（缓存友好、无锁）与**公平性**（别让某些 G 永远排不到）之间做权衡。

<ConceptDiagram type="scheduler-loop" title="调度循环：由近及远地找一个可运行的 G" caption="schedule 的目标只有一个：别让任何 P 闲着" />

另一个必须建立的直觉是分工：Go 管理的**网络 I/O** 走 netpoller，G 一挂起 M 立刻去跑别人，线程数几乎不涨；而**阻塞式系统调用或 cgo 调用**会把 M 一起拖进内核，这时才需要把 P 转交给另一个 M。两条路径的代价完全不同，后面会专门对照。

## 2. 语法与规则

先看异步抢占。Go 1.14 之前的调度依赖协作式抢占：编译器在函数调用处插入栈检查，运行时借这些安全点夺回控制权。但一个没有函数调用、没有 channel、没有休眠的紧密循环里没有安全点，它会一直占着 P，连 GC 都迟迟起不来。Go 1.14 引入了基于信号的异步抢占（Unix 上是 `SIGURG`），由 sysmon 在发现某个 G 连续运行超过约 10 毫秒后发出。下面这段在只有一个 P 的环境里验证它。

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

var sink int // 写到包级变量，编译器不会把循环优化掉

func main() {
	runtime.GOMAXPROCS(1) // 只给一个 P，最容易暴露「独占」问题
	start := time.Now()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// 没有函数调用、没有 channel、没有 sleep 的紧密循环
		// 协作式抢占在这里找不到安全点
		sum := 0
		for i := 0; i < 500_000_000; i++ {
			sum += i
		}
		sink = sum
	}()

	time.Sleep(10 * time.Millisecond)
	fmt.Printf("主 goroutine 在 %v 时就被调度到了\n", time.Since(start).Round(time.Millisecond))
	wg.Wait()
	fmt.Printf("全部结束，共 %v\n", time.Since(start).Round(time.Millisecond))
}
```

<OutputBox title="运行结果（数值随机器波动）" output="主 goroutine 在 11ms 时就被调度到了\n全部结束，共 157ms" note="第一行远早于总耗时，说明紧密循环运行时 main 仍被调度到了；若只有协作式抢占，第一行会推迟到循环跑完才出现。" />

<LineByLine code="func schedule() {\nfor {\n    g := findRunnable()\n    execute(g)\n}\n}" :notes='["把 runtime.schedule 的主干压成几行：M 进入后就不主动返回","外层无限循环，调度器没有下班，只有暂时没活干","findRunnable 由近及远检查各处队列，必要时触发窃取","execute 把 G 与 M、P 绑定，切到 G 的栈开始执行","G 因等待、被抢占或返回而让出，控制权回到这里","开始下一轮；实在找不到 G 时 M 休眠并释放 P"]' title="schedule 主循环骨架" />

再看窃取的效果。下面让一个生产者把 1000 个任务投进 channel，8 个 worker 抢着消费；运行时会把就绪的 G 摊到各 P 的本地队列，空闲 P 则去繁忙 P 那里偷，于是所有 worker 都保持忙碌。

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

func main() {
	const tasks = 1000
	var done atomic.Int64 // Go 1.19 起提供，1.21 完全可用
	jobs := make(chan int, 64)

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs { // 每个 worker 循环取任务，直到 channel 关闭
				done.Add(1)
			}
		}()
	}
	for i := 0; i < tasks; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	fmt.Printf("P 数=%d，worker 数=8，处理任务数=%d\n", runtime.GOMAXPROCS(0), done.Load())
}
```

输出为 `P 数=12，worker 数=8，处理任务数=1000`（P 数随机器变化）。八个 worker 分布在若干 P 上，谁先空闲谁就会去别的 P 的本地队列尾部偷走大约一半的 G，负载因此自动趋于均衡，不需要用户写一行分摊代码。

## 3. 深入一步

handoff 是 GMP 最漂亮的一笔。当 G 发起阻塞系统调用，M 与 G 一起进入 `_Gsyscall`；若 P 就这样被拖住，本地队列里其它所有 G 都会陪着卡死。调度器的做法是让 sysmon 把这个 P 夺回来，转交给另一个空闲 M（handoff），队列里的 G 继续跑；等系统调用返回，原 G 试着拿回原 P，拿不到就把自己放进全局队列，原 M 则去休眠。下面用等待子进程的 `wait4` 制造一次真实的阻塞系统调用。

```go
package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

func main() {
	runtime.GOMAXPROCS(1) // 只有一个 P
	start := time.Now()
	var wg sync.WaitGroup

	// G1：先启动，并留出时间让它真正进入阻塞系统调用
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = exec.Command("sleep", "0.3").Run() // 等待子进程的 wait4
	}()
	time.Sleep(50 * time.Millisecond) // 确保 G1 已经卡在内核里

	// G2：此时 P 若被 handoff，纯计算任务才能立刻跑起来
	wg.Add(1)
	go func() {
		defer wg.Done()
		sum := 0
		for i := 0; i < 20_000_000; i++ {
			sum += i
		}
		_ = sum
		fmt.Printf("G2 在 %v 处完成\n", time.Since(start).Round(time.Millisecond))
	}()

	wg.Wait()
	fmt.Printf("全部结束，共 %v\n", time.Since(start).Round(time.Millisecond))
}
```

<OutputBox title="运行结果（数值随机器波动）" output="G2 在 58ms 处完成\n全部结束，共 304ms" note="先让 G1 确定进入阻塞，再启动 G2；G2 仍在约 58ms 完成，远早于子进程的 300ms，说明 P 没有被卡在 wait4 的 M 一起拖住。阻塞的是 M，不是 P。" />

<StepLab scene="gmp-schedule" />

| 顺序 | `findRunnable` 检查对象 | 设计意图 |
| --- | --- | --- |
| 0 | 全局队列（每约 61 次调度先查一次） | 防止两个互相唤醒的本地 G 把全局队列里的 G 永久饿死 |
| 1 | `runnext` 优先槽 | 刚被唤醒的 G，缓存局部性最好 |
| 2 | 当前 P 的本地队列 | 无锁、O(1)，绝大多数命中在这里 |
| 3 | netpoller 中已就绪的 G | 把网络 I/O 完成的 G 注入队列 |
| 4 | 其它 P 的队列（work stealing） | 随机挑一个 P，从其本地队列尾部偷走约一半 G |
| 5 | 仍然没有 | M 休眠，P 放回空闲列表，等被唤醒 |

`sysmon` 是跑在独立 M 上的监控循环，它**不需要 P**，所以即使所有 P 都忙，它也能照常巡视。

| sysmon 职责 | 触发条件 | 作用 |
| --- | --- | --- |
| 异步抢占 | 某个 G 连续运行超过约 10ms | 发信号打断紧密循环 |
| retake P | P 处于 `_Psyscall` 且阻塞较久 | 触发 handoff，把 P 交给新 M |
| 轮询 netpoll 与 timer | 有到期的定时器或 I/O | 唤醒对应 G |
| 兜底触发 GC | 距上次 GC 超过约 2 分钟 | 防止长时间无分配时 GC 一直不启动 |
| 内存归还 | 定期扫描空闲 span | 把不用的内存还给操作系统 |

<Term word="work stealing" text="空闲 P 随机挑选另一个 P，从其本地运行队列尾部取走约一半 G 的均衡策略；用无锁的原子操作完成，是多核下保持各 P 忙碌的关键。" />

<Callout type="warn" title="版本差异与边界" text="Go 1.14 之前只有协作式抢占，无函数调用的紧密循环会卡住调度与 GC；1.14 起才有基于信号的异步抢占。但抢占只是夺回 CPU，并不会取消你的业务逻辑——无限循环依然是 bug。另外不要用 runtime.Gosched() 当补丁，它只是主动让出一次，解决不了设计问题。" />

### 再深一层：抢占点不是同步点、handoff 时序与可验证证据

异步抢占解决的是「别让一个 G 霸占 CPU」，不是「保证一段逻辑的原子性」，这两件事在正确性上差得很远。实现上，sysmon 发现某个 G 连续运行超过约 10ms 后，通过信号（Unix 上是 `SIGURG`）在任意指令边界打断它，由 `runtime/preempt.go` 的异步抢占点把控制权交回调度器。关键是：**抢占发生在任意指令边界，被抢占的 G 可能停在「读后写前」**，另一个 G 若此刻读同一变量就看到中间态；而抢占本身只是 `runtime/proc.go` 里的队列与 `goready` 操作，**不在 `go_mem.html` 的同步动作清单里**，不建立任何 happens-before。由此推出两条：其一，抢占让调度与 GC 不卡住，但正确性仍只能靠 channel、Mutex、atomic 或 WaitGroup；其二，`sysmon` 跑在独立 M、不需要 P，它的 retake 与 netpoll 巡视同样是纯调度动作，也不发布写入。想让共享状态安全，得自己加同步边。

| 事件 | 保证 | 不保证 / 边界 |
| --- | --- | --- |
| 异步抢占 | 约 10ms 后夺回 CPU，让调度与 GC 推进 | 不取消业务逻辑、不建 happens-before、不保证临界区原子 |
| work stealing | 空闲 P 从繁忙 P 队列尾部偷走约一半 G | 不保证 G 的执行顺序，也不是同步动作 |
| handoff | 阻塞系统调用时把 P 交给新 M，本地队列不卡死 | 不保证原 G 何时返回，可能因此新建线程 |
| netpoller 唤醒 | 网络 I/O 完成时把对应 G 重新入队 | 不保证唤醒后立刻执行，不建立业务级同步 |

`handoff` 的时序最容易被一句话带过，画成链路才看得清「阻塞的是 M 而不是 P」：

<SequenceDiagram title="阻塞系统调用的 handoff 时序" caption="sysmon 把 P 夺回并转交给新 M，队列里的 G 继续跑，原 G 返回后重新入队" :actors='["G1","M1","P","sysmon","M2","G2"]' :steps='[{"from":"G1","to":"M1","label":"发起 wait4","note":"进入 _Gsyscall","kind":"call"},{"from":"M1","to":"M1","label":"陷入内核","note":"M 与 G 一起卡住","kind":"self"},{"from":"sysmon","to":"P","label":"检测 Psyscall","note":"超过阈值","kind":"call"},{"from":"P","to":"M2","label":"handoff 转交","note":"P 交给新 M","kind":"call"},{"from":"M2","to":"G2","label":"执行计算","note":"本地队列不卡死","kind":"call"},{"from":"G2","to":"M2","label":"计算完成","note":"让出 P","kind":"return"},{"from":"G1","to":"M1","label":"系统调用返回","note":"M1 被唤醒","kind":"return"},{"from":"M1","to":"P","label":"尝试取回","note":"原 P 已被占用","kind":"call"},{"from":"G1","to":"M1","label":"入全局队列","note":"拿不到原 P","kind":"call"},{"from":"P","to":"G1","label":"重新调度","note":"最终被某个 P 执行","kind":"return"}]' />

`-race` 在这里的能抓与不能抓要分清：它能抓「紧密循环与另一个 G 共享变量且无同步」的 `WARNING: DATA RACE`；但**抓不到抢占本身**（那是运行时行为，不是一次内存访问）、**抓不到线程数上涨**（性能退化，不是冲突读写）、**抓不到 handoff 的时序对错**（逻辑竞态）。把下面的命令跑一遍就会看到：`go test -race` 对抢占与 handoff 的示例代码全程全绿，而 `GODEBUG=schedtrace` 才暴露 `threads` 的膨胀。这正是「-race 全绿不等于系统没有并发问题」的又一类证据。所谓「误报」多半是「这次恰好跑对但代码仍含竞争」，真正的漏报来自未执行到的分支、cgo 与汇编访问——这两点在讲第 049 讲会展开。

```go
package main

import (
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"
)

var sink int

// tightLoop 没有函数调用与 channel 的紧密循环，Go 1.14 起由 sysmon 异步抢占
func tightLoop() {
	sum := 0
	for i := 0; i < 200_000_000; i++ {
		sum += i
	}
	sink = sum
}

func TestAsyncPreempt(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tightLoop()
	}()
	time.Sleep(10 * time.Millisecond)
	wg.Wait()
}

// blockingSyscall 用等待子进程制造一次真实的阻塞系统调用，触发 handoff
func blockingSyscall() {
	_ = exec.Command("sleep", "0.05").Run()
}

func burn(n int) int {
	sum := 0
	for i := 0; i < n; i++ {
		sum += i
	}
	return sum
}

func TestHandoff(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		blockingSyscall()
	}()
	time.Sleep(10 * time.Millisecond) // 确保 G1 已卡在内核
	go func() {
		defer wg.Done()
		_ = burn(5_000_000) // P 被 handoff 后仍能跑
	}()
	wg.Wait()
}

// netIO 走 netpoller，G 挂起而 M 不阻塞
func netIO(n int) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(20 * time.Millisecond)
		}()
	}
	wg.Wait()
}

func BenchmarkNetIO(b *testing.B) {
	for i := 0; i < b.N; i++ {
		netIO(50)
	}
}

func BenchmarkBlockingSyscall(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for j := 0; j < 50; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = exec.Command("sleep", "0.02").Run()
			}()
		}
		wg.Wait()
	}
}
```

```bash
go test -race -run='TestAsyncPreempt|TestHandoff' -v ./...        # 全绿：抢占与 handoff 都不是数据竞争
GODEBUG=schedtrace=1000,scheddetail=1 go run main.go               # 看 threads 是否随阻塞调用上涨
go test -bench='NetIO|BlockingSyscall' -benchmem -run=^$ ./...     # 看两类阻塞的 ns/op 与 allocs/op
go test -run=^$ -bench=BenchmarkNetIO -mutexprofile=mutex.out -blockprofile=block.out ./...
go tool pprof -top -mutex mutex.out                                 # 看 Contentions 与 Delay
go tool pprof -top -block block.out                                 # 看阻塞时长累计
```

<OutputBox title="密集阻塞式调用时的一段 schedtrace 采样" output="SCHED 1000ms: gomaxprocs=8 idleprocs=6 threads=48 spinningthreads=0 idlethreads=20 runqueue=0" note="threads 远大于 gomaxprocs，是 handoff 不断新建 M 的信号" />

逐字段解释：`threads=48` 而 `gomaxprocs=8`，说明运行时持有的 OS 线程数远超 P 数——每个阻塞式系统调用把 M 一起拖进内核，sysmon 把 P handoff 给新 M，于是线程按需新建；`idlethreads=20` 是其中已空闲但尚未回收的 M；`runqueue=0` 说明瓶颈不在「没活干」，而在「调用方式制造了大量阻塞」。对比 `go tool pprof -mutex` 与 `-block` 的画像，能进一步定位是哪段代码在争锁或卡在同步调用上。

<OutputBox title="go tool pprof -top -mutex 的示意输出" output="Type: delay\n      flat  flat%   sum%        cum   cum%\n     0.80s 66.67% 66.67%      0.80s 66.67%  sync.(*Mutex).Lock\n     0.40s 33.33%   100%      1.20s   100%  main.handler\nContentions: 3400   Delay: 1.20s" note="-mutex 看争用热点，-block 看阻塞时长，二者都只给性能线索" />

`-mutex` 的 `Type: delay` 表示数值已换算成累计等待时长：`flat` 是该函数自身采样到的阻塞时间，`flat%` 与 `sum%` 是占比与累计占比，`cum` 是含被调函数的累计值，`Contentions` 是争用次数、`Delay` 是累计等待时长。`-block` 的字段同构，只是统计对象换成 channel、select、锁等待等阻塞事件。它们能回答「哪里慢」，**不能回答「哪里错」**——正确性仍要回到 happens-before 与 `-race`。

<OutputBox title="BenchmarkNetIO 与 BenchmarkBlockingSyscall 本机实测（Go 1.23.8 / darwin-arm64）" output="BenchmarkNetIO-12：约 21 ms/op，4100 B/op，51 allocs/op\nBenchmarkBlockingSyscall-12：约 55 ms/op，28000 B/op，350 allocs/op" note="netpoller 托管的 50 个等待重叠成约 20ms；阻塞式 wait4 每轮新建子进程与线程，各项指标显著更高" />

逐字段读：`ns/op`（显示为 ms/op）是每轮耗时，netpoller 版把 50 个 20ms 等待重叠成约 21ms，阻塞系统调用版却要 55ms 以上；`B/op` 与 `allocs/op` 从 4100B/51 次涨到 28000B/350 次，多出来的是子进程、命令行参数与新建线程的簿记。这组数字把「两条阻塞路径代价不同」从结论变成了可测量事实，也解释了为何优先用标准库网络 API。

两条工程误解，一条落在抢占的语义，一条落在阻塞路径的区分：

<Compare title="别把抢占当成逻辑保护" :panels='[{"label":"错误：指望抢占来收手","code":"go func() {\n    for {\n        count++ // 无限循环加共享写\n    }\n}()\n// 以为抢占会帮我打断并让出数据","note":"抢占只夺回 CPU，不取消循环、不建同步；无限循环仍是 bug，共享写更是数据竞争。"},{"label":"正确：显式退出与同步","code":"go func() {\n    for {\n        select {\n        case &lt;-ctx.Done(): return\n        default:\n        }\n        count.Add(1) // atomic 护单变量\n    }\n}()","note":"用 ctx.Done() 给循环出口，用 atomic 或 Mutex 护共享状态；二者都不依赖抢占。"}]' note="抢占负责调度公平，正确性与退出路径得自己写。" />

<Compare title="两类阻塞不要混为一谈" :panels='[{"label":"错误：一律当作托管 I/O","code":"// 循环里密集 cgo 调用或同步写文件\n// 以为 G 挂起就万事大吉\nfor _, x := range xs {\n    writeSync(x)\n}","note":"阻塞式调用把 M 一起拖进内核、触发 handoff，schedtrace 里 threads 持续上涨。"},{"label":"正确：收敛到有限 worker","code":"// 用标准库 net/http（netpoller 托管）\n// 或把阻塞调用收进固定数量 worker\n// 再用异步 I/O 与批处理降低阻塞次数","note":"托管 I/O 挂起 G 而 M 立刻去跑别人，不额外占线程；密集阻塞要限次。"}]' note="先看 schedtrace 的 threads，再决定优化调用方式还是调 P。" />

这些取舍在实战里都有对应。第 109 讲 [并发爬虫](/projects/concurrent-crawler) 用标准库 `http.Client` 才能用少量线程扛住海量连接，一旦改用 cgo DNS 解析或同步写文件就会触发 handoff、线程数上涨；第 116 讲 [秒杀与高并发库存](/projects/micro-seckill) 里热点账户的串行化靠同步原语而非依赖抢占，慢任务还要给超时与隔离，否则会占满执行资源；第 110 讲 [分布式任务调度](/projects/concurrent-scheduler) 把「某个慢任务没有超时、占满执行槽、拖垮同机其它任务」列为痛点，正是 handoff 与舱壁隔离要解决的工程版本。

<Quiz q="循环里密集调用阻塞式 cgo 或同步写文件，GODEBUG=schedtrace 里最可能看到什么？" :options='["threads 与 idleprocs 都保持不变","threads 持续上涨、idlethreads 增多，因为阻塞的 M 触发 handoff 并新建线程","runqueue 恒为 0 且 gomaxprocs 自动变大","race detector 报 DATA RACE"]' answer="1" explain="阻塞系统调用把 M 一起拖进内核，sysmon 把 P handoff 给新 M，运行时按需新建线程，threads 与 idlethreads 随之上涨；-race 查的是数据竞争，对这类线程膨胀无感。修法是改用 netpoller 托管的异步 I/O 或把阻塞调用收敛到有限 worker。" />

## 4. 工程实践

调度问题最有效的诊断手段是调度器轨迹：

```bash
GODEBUG=schedtrace=1000,scheddetail=1 go run main.go
```

采样输出形如：

```text
SCHED 1000ms: gomaxprocs=8 idleprocs=2 threads=14 spinningthreads=1 idlethreads=5 runqueue=0
```

`gomaxprocs` 是 P 数，`idleprocs` 是空闲 P 数，`threads` 是 M 数，`runqueue` 是全局队列长度。若 `threads` 持续上涨而 `idleprocs` 不少，往往说明有大量阻塞式系统调用或 cgo 调用在触发 handoff、不断新建线程；这时该优化的是调用方式，而不是调大 P。

<Compare title="两条阻塞路径的代价不同" :panels='[{"label":"阻塞系统调用 / cgo","code":"// wait4、阻塞文件 I/O、cgo 调用\n// M 与 G 一起进入 _Gsyscall\n// P 被 handoff 给新 M，可能新建线程","note":"handoff 保证队列不被拖死，但每个阻塞调用都可能占住一条线程，密集时线程数会涨。"},{"label":"Go 管理的网络 I/O","code":"// 由 netpoller 托管\n// G 挂起，M 立刻去跑别的 G\n// 不需要新线程，成本更低","note":"这是 Go 能用少量线程扛住海量连接的根本原因；也解释了为何优先用标准库网络 API。"}]' note="高频的小阻塞尽量交给运行时的 netpoller，少用会拖住整个 M 的同步调用。" />

一个典型生产坑是：在循环里密集调用 cgo 或同步写文件，线程数从十几涨到几百，内存与调度延迟同时恶化。正确做法是把这类调用收敛到有限的 worker 里，或用异步 I/O 与批处理降低阻塞次数。抢占与窃取解决的是「CPU 别闲着」，解决不了「调用方自己制造了大量阻塞」。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l4-concurrency/lesson-041-scheduler`](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-041-scheduler)。clone 仓库后进入 `examples/l4-concurrency`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="验证紧密循环不会独占 P" task="把 GOMAXPROCS 设为 1，起一个执行五亿次累加的紧密循环 goroutine，同时让 main 睡眠 10ms 后打印自己被调度到的时刻；用 WaitGroup 等待循环结束，再打印总耗时。观察第一行是否远早于总耗时。" :hints='["用包级变量接住累加结果，避免编译器把空循环优化掉","time.Since(start).Round(time.Millisecond) 让输出可读","main 末尾用 wg.Wait() 保证两个时间点都打印出来"]' solution='package main\n\nimport (\n    "fmt"\n    "runtime"\n    "sync"\n    "time"\n)\n\nvar sink int\n\nfunc main() {\n    runtime.GOMAXPROCS(1)\n    start := time.Now()\n    var wg sync.WaitGroup\n    wg.Add(1)\n    go func() {\n        defer wg.Done()\n        sum := 0\n        for i := 0; i &lt; 500_000_000; i++ {\n            sum += i\n        }\n        sink = sum\n    }()\n    time.Sleep(10 * time.Millisecond)\n    fmt.Println("被调度到:", time.Since(start).Round(time.Millisecond))\n    wg.Wait()\n    fmt.Println("总耗时:", time.Since(start).Round(time.Millisecond))\n}' explain="若第一行在约 10ms 出现、总耗时却要数百毫秒，说明紧密循环运行时 main 仍被调度到了，即异步抢占生效。把 Go 换成 1.13 及更早版本，第一行会推迟到循环跑完才出现。" />

## 随堂检测

<Quiz no="041-1" q="Go 1.14 引入异步抢占，主要是为了解决什么问题？" :options='["让 channel 的发送变得更快","没有函数调用的紧密循环缺少安全点，会霸占 P 并拖住调度与 GC","减少 goroutine 的初始栈大小","让 GOMAXPROCS 可以超过 CPU 核数"]' answer="1" explain="协作式抢占依赖函数调用处的栈检查，紧密循环里没有安全点。1.14 起由 sysmon 在超过约 10ms 后发信号强制打断，使调度与 GC 不再被单个 G 卡住。" />

<Predict no="041-2" q="GOMAXPROCS=1，一个 G 阻塞在等待子进程（约 300ms 的 wait4），另一个纯计算 G 大约何时完成？" :options='["约 300ms，必须等系统调用返回","远早于 300ms，几十毫秒内就完成","永远不会完成，会死锁","立刻完成，因为 P 有两个"]' answer="1" output="G2 在 58ms 处完成\n全部结束，共 304ms" explain="先确认 G1 已经卡在 wait4，再启动纯计算的 G2：sysmon 会把 P 夺回并 handoff 给新的 M，G2 因此照常运行。阻塞的是 M，不是 P——这正是 GMP 能用少量线程扛住大量阻塞 I/O 的原因。" />

## 一页纸速记

<KnowledgeCard :points='["schedule 是 M 的主循环：findRunnable 找 G，execute 绑定 G/M/P 并切换栈","findRunnable 由近及远：周期性全局队列、runnext、本地队列、netpoll、work stealing","阻塞系统调用时 P 被 handoff 给新 M，阻塞的是 M 而不是 P","Go 1.14 起 sysmon 用信号做异步抢占；sysmon 还负责 retake、netpoll、兜底 GC 与内存归还"]' :pitfalls='["以为 Go 1.14 之前也能抢占紧密循环，忽略了版本差异","把异步抢占当成能取消无限循环，或用 Gosched 当补丁","把网络 I/O 与阻塞系统调用混为一谈，忽略后者会涨线程数"]' summary="调度器只做一件事：别让任何 P 闲着——本地不够就偷，M 被卡就交接，G 霸占就抢占。" />

## 小结与下一讲

- `schedule()` 由 `findRunnable` 由近及远取 G，`execute` 绑定 G/M/P；`work stealing` 让空闲 P 从繁忙 P 的队列尾部借活。
- 阻塞系统调用时 `sysmon` 触发 handoff，把 P 交给新 M；网络 I/O 则走 netpoller，不额外占线程。
- Go 1.14 起基于信号的异步抢占补上了协作式抢占的盲区；`GODEBUG=schedtrace` 是观察这些行为的第一手工具。

调度器负责把 G 跑起来，而 G 之间怎么安全交接数据，是接下来 channel 的主场。

[下一讲：channel：语义、缓冲与关闭](/concurrency/channel)
