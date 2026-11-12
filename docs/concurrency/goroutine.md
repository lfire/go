---
title: 第 39 讲 · goroutine：创建、栈与生命周期
description: 第 39 讲 · goroutine：创建、栈与生命周期 —— go 关键字、初始栈与增长、调度单位、泄漏与排查、GOMAXPROCS
---

<StageHeader stage="L4 · 并发编程" level="★★☆☆☆" time="35 分钟" goal="知道协程轻在哪里、会怎么泄漏" prereq="第 38 讲 · 并发与并行：CSP 思想" />

# 第 39 讲 · goroutine：创建、栈与生命周期

> 上一讲我们确认了一件事：并发的收益来自重叠等待。这一讲看承担这些等待的最小执行单元——goroutine。它常常被叫作「轻量线程」，但「轻」到底轻在哪、能开多少个、什么时候会悄悄赖着不走，是写出稳定服务必须回答的问题。搞懂这三点，你才会明白为什么 Go 敢给每个请求随手开一个 goroutine。

## 这一讲你会得到什么

<CheckList id="L39-goal" title="本讲目标" :items='["用 go 关键字正确启动任务，并知道主 goroutine 不会替你等待","说清 goroutine 初始栈、按需增长与线程栈的差别","理解 G 是最小的调度单位，与 M、P 的关系是什么","能识别并排查 goroutine 泄漏，理解 GOMAXPROCS 限制的是并行度"]' />

## 1. 先建立直觉

把操作系统线程想成给每位客人租一整间酒店客房：房间面积（栈）在入住时就按 MB 量级划好，退不退、什么时候换房都由前台（内核）统一调度，换一次房要过一道系统调用。goroutine 则像共享大厅里的一把折叠椅：初始只占约 2 KB 的栈，用到不够了由 Go 运行时自动搬到更大的连续空间，切换时只在用户态换一下栈指针和几个寄存器。栈会按需增长也会在函数返回后回收，这就是「轻」的本质——不是没有成本，而是起始成本极低、且不必惊动内核。

于是数量级完全不同：一台普通机器上开几千个 OS 线程往往就开始吃力，而开十万个 goroutine 只是常规操作。更关键的是，goroutine 不绑定某个线程：运行时把它放到队列里，哪个线程空了就由哪个线程捡起来跑。这就是后面 GMP 模型里 G 的位置——G 只是「一段待执行的栈加寄存器现场」，是调度的最小单位。

<ConceptDiagram type="gmp" title="goroutine 就是 G：由 M 在持有 P 时执行" caption="G 不绑定线程，M 必须持有 P 才能运行 G" />

一个必须立刻建立的直觉是：goroutine 之间没有父子关系。`main` 本身也是一个 goroutine，它一返回，整个进程就结束，运行时不会替你等任何还在跑的 G。所以「起了 goroutine 却没看见输出」几乎总是同一个原因：你没等它。

## 2. 语法与规则

`go` 关键字把一次函数调用放进新建的 goroutine，语句本身**立即返回**，不会阻塞当前流程。想让它干完，需要显式同步；最常用的是 `sync.WaitGroup`。

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fmt.Println("goroutine: 我开始工作")
		time.Sleep(50 * time.Millisecond)
		fmt.Println("goroutine: 工作完成")
	}()
	fmt.Println("main: go 语句立刻返回，我不会等它")
	wg.Wait() // 删掉这一行，main 直接退出，上面两行很可能看不到
	fmt.Println("main: 结束")
}
```

<OutputBox title="运行结果（前两行顺序可能互换）" output="main: go 语句立刻返回，我不会等它\ngoroutine: 我开始工作\ngoroutine: 工作完成\nmain: 结束" note="main 与新建 goroutine 谁先打印由调度器决定；但「工作完成」一定在「main: 结束」之前，因为 Wait 在等它。" />

<LineByLine code="var wg sync.WaitGroup\nwg.Add(1)\ngo func() {\n    defer wg.Done()\n    work()\n}()\nwg.Wait()" :notes='["声明一个 WaitGroup，本质是等待一组 goroutine 结束的计数器","计数加一，必须在启动 goroutine 之前完成，否则 Wait 可能提前返回","go 关键字把函数调用放进新的 goroutine，这条语句本身立刻返回","用 defer 保证无论函数怎么结束都会把计数减一","真正的业务逻辑，运行在新建 goroutine 自己的栈上","闭包结束，这个 goroutine 的生命周期随之终止，栈被回收","main 在这里阻塞到计数归零，否则它退出时会带走所有 G"]' title="go 与 WaitGroup 三件套" />

第二段代码用数据回答「能开多少」：一次性创建十万个 goroutine，各自做一件极小的事，等待全部结束后再看还剩下几个。

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
)

func main() {
	const n = 100000
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			// 每个 G 只做一件小事，返回后栈被回收、G 可被复用
			_ = 1 + 1
		}()
	}
	wg.Wait()
	fmt.Printf("成功创建并回收 %d 个 goroutine\n", n)
	fmt.Printf("当前仍存活: %d 个\n", runtime.NumGoroutine())
}
```

输出约为：`成功创建并回收 100000 个 goroutine`，随后 `当前仍存活: 1 个`（有时是 2 个，多出来的是运行时自己起的辅助 G）。十万个并发执行单元在普通笔记本上就能跑完，换成同量级 OS 线程是不可想象的。

## 3. 深入一步

「轻」也带来一个副作用：起 goroutine 太便宜，以至于忘了收尾也不会立刻报错。下面这段故意制造一个泄漏，并用 `runtime.NumGoroutine()` 把它照出来。

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

// leak 故意启动一个永远等不到数据的 goroutine
func leak() {
	ch := make(chan int)
	go func() {
		v := <-ch // 没有任何发送者，这里永久阻塞
		fmt.Println("永远不会执行:", v)
	}()
}

func main() {
	fmt.Println("启动前 goroutine 数:", runtime.NumGoroutine())
	leak()
	time.Sleep(10 * time.Millisecond) // 给调度器时间把新 G 挂起
	fmt.Println("泄漏后 goroutine 数:", runtime.NumGoroutine())
	// 排查手段：pprof 的 goroutine profile、go tool trace，
	// 以及给每一条阻塞路径配上超时或取消信号。
}
```

输出稳定为 `启动前 goroutine 数: 1` 与 `泄漏后 goroutine 数: 2`。这个多出来的 1 不会随 main 结束之外的任何逻辑消失：它占着栈、挂在等待队列里，直到进程退出。**泄漏不是崩溃，是缓慢失血**——服务跑几天后 goroutine 数一路涨，内存与调度开销随之上升。

| 维度 | OS 线程 | goroutine |
| --- | --- | --- |
| 创建与调度者 | 内核 | Go 运行时（用户态） |
| 初始栈 | 以 MB 计，通常固定 | 约 2 KB，按需增长 |
| 栈上限 | 基本固定 | 64 位默认约 1 GB |
| 切换成本 | 微秒级，需陷入内核 | 纳秒级，只换栈与少量寄存器 |
| 是否绑定 | 一个线程一套资源 | 不绑定线程，可被任意 M 捡起 |
| 单机常见规模 | 几千 | 十万级 |

一个 G 的典型生命周期依次经过这些状态：

| 状态 | 含义 |
| --- | --- |
| `_Grunnable` | 已就绪，排在某个运行队列里等 P 挑中 |
| `_Grunning` | 正绑定在某个 M 上执行用户代码 |
| `_Gsyscall` | 正陷在阻塞系统调用里，M 与它一起被卡住 |
| `_Gwaiting` | 因 channel、锁或定时器挂起，等别人唤醒 |
| `_Gdead` | 函数已返回，G 与栈可被复用 |

<Term word="可增长栈" text="goroutine 初始只分配约 2KB 栈，函数栈帧不够时运行时会分配更大的连续栈并拷贝过去，对使用者完全透明。" />

<Callout type="warn" title="三个高频易错点" text="其一，main 返回会结束整个进程，不会等待任何 goroutine；其二，G 之间没有父子关系，也没有「杀掉某个 G」的 API；其三，只要有一条阻塞路径永远等不到唤醒，就是泄漏。示例程序故意泄漏以便演示，真实代码必须给每条阻塞留退出路径。" />

`GOMAXPROCS` 常被误解成「最多能开多少 goroutine」，其实它限制的是**并行度**：同一时刻最多有多少个 P 在执行 Go 代码。默认值通常等于机器的逻辑 CPU 数，Go 1.25 起在容器里还会参考 cgroup 的 CPU 限额来定默认值。你可以创建十万个 G，但同一瞬间最多只有 `GOMAXPROCS` 个在跑；I/O 密集任务因为大量时间在等待，往往比 CPU 密集任务更能从多出来的 P 中获益。

## 4. 工程实践

标准库 `net/http` 的做法就是本讲最典型的落地：每个连接由独立 goroutine 服务，请求结束 G 即退出，不需要线程池。这种模式成立的前提正是「创建便宜、阻塞便宜、回收自动」。工程上要注意三点：给所有后台 goroutine 留可取消的退出路径；用监控持续观察 `runtime.NumGoroutine()` 的趋势而不是单点值；出现堆积时用 `go tool pprof` 的 goroutine profile 看阻塞在哪一行。

<Compare title="同一个 worker，有没有退出路径" :panels='[{"label":"泄漏写法","code":"go func() {\n    for range ch {\n    }\n}()\n// 若 ch 永不关闭，range 永不结束","note":"阻塞路径没有终止条件，G 会一直挂在等待队列里，随请求数线性增长。"},{"label":"有退出路径","code":"go func() {\n    defer wg.Done()\n    for v := range ch {\n        handle(v)\n    }\n}()\nclose(ch)","note":"唯一的发送方结束时关闭 channel，range 自然收尾，defer 保证计数归还。"}]' note="约定「谁发送、谁关闭」，让每条阻塞路径都能被唤醒，是防泄漏最有效的一条纪律。" />

不要自己造「goroutine 池」来复用 G：语言已经把创建成本压到极低，池化只会引入排队、超时与归还逻辑，反而更容易泄漏。真正需要限制的是并发**数量**（例如同时最多 20 个外部请求），那应该用带缓冲的 channel 或信号量显式限流，而不是池化执行单元本身。

## 动手练一练

<Exercise title="用 NumGoroutine 观察生命周期" task="写一个程序：先打印启动前的 goroutine 数，再启动 5 个各睡 10ms 的 goroutine，打印启动后的数量，最后用 WaitGroup 等待并打印结束后的数量。思考为什么结束后数量会回落。" :hints='["runtime.NumGoroutine() 返回当前存在的 G 总数，包含 main 自己","wg.Add(5) 可以一次加满，也可以每次循环加 1","睡 10ms 是为了让「启动后」这次采样能看到它们还活着"]' solution='package main\n\nimport (\n    "fmt"\n    "runtime"\n    "sync"\n    "time"\n)\n\nfunc main() {\n    fmt.Println("启动前:", runtime.NumGoroutine())\n    var wg sync.WaitGroup\n    wg.Add(5)\n    for i := 0; i &lt; 5; i++ {\n        go func() {\n            defer wg.Done()\n            time.Sleep(10 * time.Millisecond)\n        }()\n    }\n    fmt.Println("启动后:", runtime.NumGoroutine())\n    wg.Wait()\n    fmt.Println("结束后:", runtime.NumGoroutine())\n}' explain="启动前通常只有 main 一个 G，启动后变成 6 个，Wait 之后五个 G 的函数返回、状态变为可被复用的终止态，计数回落到 1。若结束后数字没有回落，就说明有 G 还挂在某条阻塞路径上。" />

## 随堂检测

<Quiz no="39-1" q="关于 goroutine 的栈，下面哪一项是正确的？" :options='["每个 goroutine 生来就有 8MB 固定栈，与线程一样","初始栈约 2KB，按需自动增长，函数返回后会被回收","栈只增不减，运行过的 goroutine 的栈永远不会释放","goroutine 栈由内核分配，创建必须陷入内核态"]' answer="1" explain="Go 运行时在用户态给每个 G 分配约 2KB 的初始栈，不够时自动搬去更大的连续栈；函数返回后 G 终止、栈被回收，创建过程不需要系统调用。" />

<Predict no="39-2" q="main 里用循环起了 3 个执行 fmt.Println(i) 的 goroutine，随后函数直接结束，最可能看到什么？" :options='["稳定打印 0 1 2","大概率什么都不打印","固定打印三行，只是顺序随机","编译报错：goroutine 不能被丢弃"]' answer="1" output="（大概率没有任何输出，程序直接退出）" explain="main 也是一个 goroutine，它一返回进程就结束，运行时不会等待其它 G。想让输出出现，必须用 WaitGroup、channel 或其它同步手段显式等待；这也是「起了协程却看不到日志」的头号原因。" />

## 一页纸速记

<KnowledgeCard :points='["go 关键字立刻返回，新建 G 由运行时择机执行；main 不等任何 G","初始栈约 2KB、按需增长、返回即回收，创建与切换都在用户态","G 是最小调度单位，不绑定线程；GOMAXPROCS 限制并行度而非 G 的数量","用 WaitGroup 显式等待，用 NumGoroutine 与 goroutine profile 排查堆积"]' :pitfalls='["删掉 Wait 就以为任务会自己跑完，输出莫名其妙消失","后台 G 阻塞在无人唤醒的 channel 上，形成慢性泄漏","把 GOMAXPROCS 当成 goroutine 数量上限，或盲目手搓 goroutine 池"]' summary="goroutine 轻在栈与调度，重在收尾：起得便宜，更要保证每条阻塞路径都等得到唤醒。" />

## 小结与下一讲

- `go` 语句立即返回，新 G 由运行时调度；`main` 退出会结束整个进程，必须显式等待。
- G 初始栈约 2 KB、按需增长、返回即回收，不绑定线程，十万级规模很常见；`GOMAXPROCS` 管的是并行度。
- 泄漏的根源是「永久阻塞却无人唤醒」；靠关闭约定、取消信号与 goroutine profile 防住它。

既然 G 要被「谁」挑起来执行，下一讲就把 G、M、P 三者摊开，画出完整的调度模型。

[下一讲：GMP 调度模型全景](/concurrency/gmp)
