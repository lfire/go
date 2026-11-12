---
title: 第 41 讲 · 调度循环：抢占、窃取与 handoff
description: 第 41 讲 · 调度循环：抢占、窃取与 handoff —— schedule 循环、work stealing、系统调用与 handoff、异步抢占、sysmon 职责
---

<StageHeader stage="L4 · 并发编程" level="★★★★★" time="50 分钟" goal="理解调度器如何保持 CPU 忙碌" prereq="第 40 讲 · GMP 调度模型全景" />

# 第 41 讲 · 调度循环：抢占、窃取与 handoff

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

## 动手练一练

<Exercise title="验证紧密循环不会独占 P" task="把 GOMAXPROCS 设为 1，起一个执行五亿次累加的紧密循环 goroutine，同时让 main 睡眠 10ms 后打印自己被调度到的时刻；用 WaitGroup 等待循环结束，再打印总耗时。观察第一行是否远早于总耗时。" :hints='["用包级变量接住累加结果，避免编译器把空循环优化掉","time.Since(start).Round(time.Millisecond) 让输出可读","main 末尾用 wg.Wait() 保证两个时间点都打印出来"]' solution='package main\n\nimport (\n    "fmt"\n    "runtime"\n    "sync"\n    "time"\n)\n\nvar sink int\n\nfunc main() {\n    runtime.GOMAXPROCS(1)\n    start := time.Now()\n    var wg sync.WaitGroup\n    wg.Add(1)\n    go func() {\n        defer wg.Done()\n        sum := 0\n        for i := 0; i &lt; 500_000_000; i++ {\n            sum += i\n        }\n        sink = sum\n    }()\n    time.Sleep(10 * time.Millisecond)\n    fmt.Println("被调度到:", time.Since(start).Round(time.Millisecond))\n    wg.Wait()\n    fmt.Println("总耗时:", time.Since(start).Round(time.Millisecond))\n}' explain="若第一行在约 10ms 出现、总耗时却要数百毫秒，说明紧密循环运行时 main 仍被调度到了，即异步抢占生效。把 Go 换成 1.13 及更早版本，第一行会推迟到循环跑完才出现。" />

## 随堂检测

<Quiz no="41-1" q="Go 1.14 引入异步抢占，主要是为了解决什么问题？" :options='["让 channel 的发送变得更快","没有函数调用的紧密循环缺少安全点，会霸占 P 并拖住调度与 GC","减少 goroutine 的初始栈大小","让 GOMAXPROCS 可以超过 CPU 核数"]' answer="1" explain="协作式抢占依赖函数调用处的栈检查，紧密循环里没有安全点。1.14 起由 sysmon 在超过约 10ms 后发信号强制打断，使调度与 GC 不再被单个 G 卡住。" />

<Predict no="41-2" q="GOMAXPROCS=1，一个 G 阻塞在等待子进程（约 300ms 的 wait4），另一个纯计算 G 大约何时完成？" :options='["约 300ms，必须等系统调用返回","远早于 300ms，几十毫秒内就完成","永远不会完成，会死锁","立刻完成，因为 P 有两个"]' answer="1" output="G2 在 58ms 处完成\n全部结束，共 304ms" explain="先确认 G1 已经卡在 wait4，再启动纯计算的 G2：sysmon 会把 P 夺回并 handoff 给新的 M，G2 因此照常运行。阻塞的是 M，不是 P——这正是 GMP 能用少量线程扛住大量阻塞 I/O 的原因。" />

## 一页纸速记

<KnowledgeCard :points='["schedule 是 M 的主循环：findRunnable 找 G，execute 绑定 G/M/P 并切换栈","findRunnable 由近及远：周期性全局队列、runnext、本地队列、netpoll、work stealing","阻塞系统调用时 P 被 handoff 给新 M，阻塞的是 M 而不是 P","Go 1.14 起 sysmon 用信号做异步抢占；sysmon 还负责 retake、netpoll、兜底 GC 与内存归还"]' :pitfalls='["以为 Go 1.14 之前也能抢占紧密循环，忽略了版本差异","把异步抢占当成能取消无限循环，或用 Gosched 当补丁","把网络 I/O 与阻塞系统调用混为一谈，忽略后者会涨线程数"]' summary="调度器只做一件事：别让任何 P 闲着——本地不够就偷，M 被卡就交接，G 霸占就抢占。" />

## 小结与下一讲

- `schedule()` 由 `findRunnable` 由近及远取 G，`execute` 绑定 G/M/P；`work stealing` 让空闲 P 从繁忙 P 的队列尾部借活。
- 阻塞系统调用时 `sysmon` 触发 handoff，把 P 交给新 M；网络 I/O 则走 netpoller，不额外占线程。
- Go 1.14 起基于信号的异步抢占补上了协作式抢占的盲区；`GODEBUG=schedtrace` 是观察这些行为的第一手工具。

调度器负责把 G 跑起来，而 G 之间怎么安全交接数据，是接下来 channel 的主场。

[下一讲：channel：语义、缓冲与关闭](/concurrency/channel)
