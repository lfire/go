---
title: 第 38 讲 · 并发与并行：CSP 思想
description: 第 38 讲 · 并发与并行：CSP 思想 —— 并发不等于并行、CSP 与共享内存的取舍、Go 的选择、何时不该并发
---

<StageHeader stage="L4 · 并发编程" level="★★☆☆☆" time="30 分钟" goal="建立正确的并发世界观" prereq="阶段三 · 核心机制" />

# 第 38 讲 · 并发与并行：CSP 思想

> 从这里开始进入 Go 最迷人、也最容易写出隐蔽 bug 的领域。很多人以为「用了 goroutine 程序就变快了」，结果只是把顺序执行改成了更难调试的顺序执行。先别急着写 `go`，我们先把「并发」和「并行」分清楚，再看 Go 为什么把 CSP 当成第一公民。学完你会发现，决定要不要并发的从来不是语法，而是任务里究竟有没有「可以重叠的等待」。

## 这一讲你会得到什么

<CheckList id="L38-goal" title="本讲目标" :items='["能说清并发与并行的区别，并解释单核为何也能并发","理解 CSP 通过通信共享内存与共享内存加锁两条路线的差异","知道 Go 的取舍：goroutine 加 channel 是首选，sync 包仍然保留","能判断哪些场景不该引入并发，避免为了并发而并发"]' />

## 1. 先建立直觉

先用一家咖啡店做类比。一个咖啡师、一台咖啡机：他先接单，把牛奶放上蒸汽棒加热，在等待的三十秒里去磨下一杯的豆子，再回头拿牛奶——这是**并发**。他同时应对「加热」「磨豆」「接单」好几件事，但任一瞬间只做一件。若店里雇了两个咖啡师、两台机器，两人真的在同一时刻各干各的——这才是**并行**。Rob Pike 那句被广泛引用的话说的正是这件事：并发是关于「同时应对很多事」的结构，并行是关于「同时做很多事」的执行。并行需要多个执行单元，并发不需要；单核 CPU 上跑得很顺的并发程序，搬到多核上才有机会真正并行。

理解这一点，就能解释一个常见困惑：把三个各等待 100 毫秒的网络请求改成三个 goroutine，总耗时从约 300 毫秒降到约 100 毫秒，快的是「等待被重叠」，不是 CPU 变快了。反过来，若任务是纯计算、没有一丝等待，且机器只有一个可用核心，那么并发不会带来加速，只会多出调度与协调的开销。

CSP 是 Communicating Sequential Processes 的缩写，出自 Tony Hoare 1978 年的论文。它的主张很干净：每个进程本身都是**顺序**的、各自持有自己的数据，进程之间不共享内存，只通过显式的通道传递消息。Go 把它浓缩成那句谚语：不要用共享内存来通信，而要用通信来共享内存。对比另一条路线——多线程直接读写同一块内存，再用互斥锁圈出临界区——两者都能写出正确程序，但心智负担不同：共享内存要你时刻记住「谁在什么时候能碰这块数据」，CSP 则把数据所有权随消息一起交出去，交接点清晰可见。

<ConceptDiagram type="pipeline" title="CSP：独立的顺序阶段由通道串成一条流水线" caption="每个阶段只管自己的数据，通道是唯一的交界处" />

Go 的取舍很务实：它把 goroutine 与 channel 做成语言级设施，鼓励你优先用通信来组织并发；但它并没有删掉互斥锁、原子操作这些共享内存工具。原因很现实——「只是想保护一个计数器」这类场景，用锁比绕一圈 channel 更直白。语言与工具箱的关系是「给一个好用的默认值，但不堵死其它路」，而不是逼你二选一。

## 2. 语法与规则

先用一个可测量的例子看并发到底带来了什么。下面用 `time.Sleep` 模拟 I/O 等待，用 `sync.WaitGroup` 等待所有任务结束；两次执行除了「串行还是并发」之外完全相同。

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

// task 模拟一次耗时 100ms 的 I/O 操作，期间不占用 CPU
func task(id int) {
	time.Sleep(100 * time.Millisecond)
	fmt.Printf("任务 %d 完成\n", id)
}

func main() {
	// 串行：三个任务一个接一个，等待时间累加
	start := time.Now()
	for i := 1; i <= 3; i++ {
		task(i)
	}
	fmt.Printf("串行耗时: %v\n", time.Since(start).Round(time.Millisecond))

	// 并发：三个任务同时开始，等待时间重叠
	start = time.Now()
	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			task(id)
		}(i)
	}
	wg.Wait()
	fmt.Printf("并发耗时: %v\n", time.Since(start).Round(time.Millisecond))
}
```

<OutputBox title="运行结果（goroutine 的完成顺序不固定）" output="任务 1 完成\n任务 2 完成\n任务 3 完成\n串行耗时: 304ms\n任务 2 完成\n任务 3 完成\n任务 1 完成\n并发耗时: 101ms" note="耗时带上少量调度开销，会在 300ms 与 100ms 上下浮动，但约三倍的差距稳定；任务打印顺序由调度器决定。" />

<LineByLine code="ch := make(chan int, 2)\ngo produce(ch)\nfor v := range ch {\n    fmt.Println(v)\n}" :notes='["创建一个能暂存 2 个元素的缓冲 channel，容量就是允许积压的消息数","启动生产者 goroutine，它负责填入数据并在结束时关闭 channel","range 会持续接收，直到 channel 被关闭且缓冲被取空","每次循环拿到的值是通过通道交接过来的独立副本","循环自然结束意味着不会再有数据，消费者不必自己判断终止条件"]' title="CSP 写法的骨架" />

再看 CSP 的核心写法：生产者把数据的所有权交给 channel，主 goroutine 只负责接收，全程没有任何共享变量，也不需要一把锁。

```go
package main

import "fmt"

// produce 是唯一的生产者，只通过参数里的 channel 把数据交出去
func produce(ch chan<- int) {
	for i := 1; i <= 3; i++ {
		ch <- i * i // 发送完成，这个值的所有权就转移给了接收方
	}
	close(ch) // 发送方负责关闭，通知接收方没有更多数据
}

func main() {
	ch := make(chan int)
	go produce(ch)
	// 主 goroutine 不碰任何共享内存，只从通道里取
	for v := range ch {
		fmt.Println("收到:", v)
	}
}
```

运行结果稳定为三行：`收到: 1`、`收到: 4`、`收到: 9`。注意 `chan int` 是双向通道，可以隐式转换成函数签名要求的 `chan<- int`（只发送）；反过来则不行，这正是编译期替我们守住的所有权约定。

## 3. 深入一步

第三段代码回答「单核到底能不能并发」：把 `GOMAXPROCS` 设为 1，只留一个执行 Go 代码的逻辑处理器，三个 goroutine 依然会交替推进。

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
)

func main() {
	runtime.GOMAXPROCS(1) // 只有一个 P，任何时刻至多一个 goroutine 在执行

	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 1; j <= 2; j++ {
				fmt.Printf("goroutine %d 第 %d 步\n", id, j)
				runtime.Gosched() // 主动让出，让同一线程上的其它 goroutine 也跑一步
			}
		}(i)
	}
	wg.Wait()
	fmt.Println("全部完成：单核上它们是并发，但不是并行")
}
```

输出里三个编号会交替出现，具体先后顺序每次可能不同，但「都跑完了」这一点确定无疑。这说明并发是一种**程序结构**：把工作拆成可独立推进的活动；并行是**运行时状态**：这些活动恰好在同一时刻一起跑。

| 维度 | 串行 | 并发 | 并行 |
| --- | --- | --- | --- |
| 含义 | 一件事做完再做下一件 | 同时应对多件可独立推进的事 | 同一时刻真的在多件上运行 |
| 单核能否实现 | 能 | 能 | 不能 |
| 关键收益 | 简单、无协调成本 | 重叠等待、提高响应与吞吐 | 缩短纯计算的总时间 |
| 典型代价 | 等待被浪费 | 调度、同步与排查复杂度 | 受执行单元数量限制 |

<Term word="CSP" text="Communicating Sequential Processes：顺序进程之间不共享内存，只通过通道传递消息来协作的并发模型。" />

<Callout type="warn" title="最容易混淆的一点" text="并发不等于更快。若任务之间没有可重叠的等待，或者任务太小、拆分与协调的成本超过收益，引入 goroutine 只会让代码变慢且更难调试。先问「等待在哪里」，再决定要不要并发。" />

## 4. 工程实践

真实服务里最常见的收益来自 I/O：一个 HTTP handler 往往要查数据库、调下游接口、写日志，这些等待彼此独立，用 goroutine 重叠起来就能显著降低尾延迟。而「保护一个共享计数」「缓存一张配置表」这类需求，本质是互斥而不是传递数据，用 `sync.Mutex` 或原子操作更直接。判断口诀是：要交付数据所有权、要编排阶段、要广播信号，选 channel；只想保护一小段临界区，选锁。

<Compare title="两条路线的对照" :panels='[{"label":"共享内存 + 锁","code":"var mu sync.Mutex\nvar count int\nmu.Lock()\ncount++\nmu.Unlock()","note":"多个执行流直接读写同一变量，靠锁圈出临界区；要自己保证每次访问都记得加锁。"},{"label":"CSP 通道","code":"ch := make(chan int, 1)\n// 数据沿通道交接\n// 所有权随消息转移","note":"数据沿通道流动，交接点即同步点；但为了一个计数器绕一圈通道反而更绕。"}]' note="没有绝对正确的选择，只有与问题匹配的选择：传递数据用通道，保护状态用锁。" />

还有一类明确的「不该并发」：步骤之间有严格先后依赖（必须先解析配置再启动服务）、单次工作量极小（微秒级计算）、以及 CPU 已经跑满且没有等待的纯计算流水线。这几种情况下，并发带来的上下文切换与内存占用是净亏损。工程上更稳妥的做法是先写正确的串行版本，用压测或 `pprof` 找到真正卡在等待上的瓶颈，再局部引入并发。

## 动手练一练

<Exercise title="把串行执行改成并发执行" task="有三个编号 1、2、3 的工作函数，请改成用 goroutine 并发执行，并保证 main 在三者全部结束后才退出。要求用 sync.WaitGroup，且不要把循环变量直接捕获进闭包。" :hints='["wg.Add(1) 必须写在 go 语句之前","把编号作为函数参数传进闭包，而不是在闭包里直接引用循环变量","main 的最后一行调用 wg.Wait()"]' solution='package main\n\nimport (\n    "fmt"\n    "sync"\n)\n\nfunc work(id int) {\n    fmt.Println("任务", id, "完成")\n}\n\nfunc main() {\n    var wg sync.WaitGroup\n    for i := 1; i &lt;= 3; i++ {\n        wg.Add(1)\n        go func(id int) {\n            defer wg.Done()\n            work(id)\n        }(i)\n    }\n    wg.Wait()\n    fmt.Println("全部结束")\n}' explain="Add 在 go 之前计数，Done 用 defer 保证异常路径也能减一，Wait 让 main 等所有 goroutine 收尾。把 i 作为参数传入闭包，可避开老版本 Go 的循环变量捕获陷阱；若删掉 Wait，main 会直接退出，三个任务很可能一行都来不及打印。" />

## 随堂检测

<Quiz no="38-1" q="关于并发与并行，下面哪一种说法是准确的？" :options='["并发必须有多核 CPU 才能实现","并行是并发的子集，先有并行才有并发","并发是关于结构与应对多件事，并行是关于同一时刻真的在做多件事","只要用了 goroutine，程序就一定在并行执行"]' answer="2" explain="并发描述程序被组织成多个可独立推进的活动，单核上通过交替执行也能做到；并行是这些活动在同一时刻真的同时运行，需要多个执行单元。二者是不同维度，不存在包含关系。" />

<Predict no="38-2" q="把三个各等待 100ms 的 I/O 任务从串行改成三个 goroutine 并发执行，总耗时最接近哪个值？" :options='["仍约 300ms","约 100ms","约 33ms","一定比串行更慢"]' answer="1" output="串行耗时: 300ms\n并发耗时: 100ms" explain="等待时间可以重叠，三段 100ms 的等待合并成约 100ms。缩短的是等待而不是计算；若三个任务都是占满 CPU 的纯计算且只有一个 P，并发就不会带来这个收益。" />

## 一页纸速记

<KnowledgeCard :points='["并发是结构（同时应对多件事），并行是执行状态（同一时刻真的在做多件事），单核也能并发","CSP 主张顺序进程不共享内存，只通过通道传递消息与所有权；共享内存路线靠锁圈临界区","Go 的默认答案是 goroutine 加 channel，但保留 sync 与 atomic 用于纯互斥场景","并发的收益来自重叠等待，先找到等待再并发"]' :pitfalls='["误以为用了 goroutine 就一定更快，忽略纯计算与协调开销","把「保护一个计数器」硬绕成通道，增加无谓复杂度","步骤有严格先后依赖、任务极小、CPU 已跑满时仍然盲目并发"]' summary="并发是关于怎么组织，CSP 是关于怎么交接：先分清等待与计算，再选通道或锁。" />

## 小结与下一讲

- 并发与并行是两个维度：前者是应对多件事的结构，后者是同时运行的状态，单核照样能并发。
- CSP 用通道传递数据所有权，交接点天然是同步点；共享内存加锁则适用于纯互斥的小临界区，Go 两者都保留。
- 并发的收益来自重叠等待；没有等待、有严格依赖或任务过小时，串行版本往往是更好的工程选择。

下一讲我们落到具体的执行单元上：goroutine 到底轻在哪里、栈怎么长、什么时候会泄漏。

[下一讲：goroutine：创建、栈与生命周期](/concurrency/goroutine)
