---
title: 第 40 讲 · GMP 调度模型全景
description: 第 40 讲 · GMP 调度模型全景 —— G/M/P 各自职责、全局与本地队列、P 的数量、调度上下文、一张图记住模型
---

<StageHeader stage="L4 · 并发编程" level="★★★★☆" time="50 分钟" goal="画出 G、M、P 的关系图" prereq="第 39 讲 · goroutine：创建、栈与生命周期" />

# 第 40 讲 · GMP 调度模型全景

> 上一讲我们把 goroutine 拆成了 G：一段栈、一组寄存器现场、一个状态。但 G 自己不会跑，它得被谁挑起来、排在哪条队里、由哪条线程执行，这些问题都属于调度模型。GMP 是理解 Go 并发的分水岭：在这之前你只是「会用 go」，在这之后你才能解释「为什么这样设计」「为什么高并发下它不塌」。这一讲只画结构与职责，把抢占、窃取与交接留给下一讲。

## 这一讲你会得到什么

<CheckList id="L40-goal" title="本讲目标" :items='["说清 G、M、P 各自的本质与职责，以及 M 为何必须持有 P","理解本地运行队列与全局队列的分工和取舍","知道 P 的数量由 GOMAXPROCS 决定，以及它限制的是什么","理解 P 作为调度上下文为何能消除全局锁竞争","能画出一张完整的 GMP 关系图"]' />

## 1. 先建立直觉

把调度器想成一座工厂。**G** 是一张张工单：上面写着要执行的函数、栈和当前状态，本身不占 CPU。**M** 是真正的工人，也就是操作系统线程，只有它能把指令喂给 CPU。**P** 是工位加工具箱：它不是 CPU，也不是线程，而是「一套允许开工的资源与权限」——本地待办队列、一个优先槽位、内存分配缓存，都挂在 P 上。工人 M 必须领到一个工位 P，才能从队列里取工单 G 来干；没有 P 的 M 只能歇着或去干运行时杂活。

这个「工位」不是一开始就有的。Go 1.0 用的是 GM 模型：所有 M 都去同一条全局队列抢 G，抢之前要争同一把全局锁。核数一多，锁本身就成了瓶颈，而且 M 一旦因系统调用阻塞，它手里那批 G 会跟着一起卡住。Go 1.1 引入 P（出自 Dmitry Vyukov 的 Scalable Go Scheduler 设计），把「取任务」这件事分散到每个 P 私有的本地队列：绝大多数调度在本地无锁完成，全局锁只在少数兜底路径上出现。P 同时让「把工位转交给另一个工人」成为可能——这正是下一讲 handoff 的前提。

<ConceptDiagram type="gmp" title="GMP 全景：M 必须持有 P 才能执行 G" caption="P 是调度上下文，数量由 GOMAXPROCS 决定" />

先把一句话记牢：**G 要跑，必须同时有一个 M 和一个被该 M 持有的 P**。三者绑定时 G 处于运行态；G 一旦等待、阻塞或结束，M 就切回调度器去取下一张工单。切换发生在用户态，只换栈指针与少量寄存器，成本远低于线程切换。

## 2. 语法与规则

P 的数量不是玄学，可以直接查询和设置。`runtime.GOMAXPROCS(0)` 是查询惯例：传 0 只返回当前值，不修改配置。

```go
package main

import (
	"fmt"
	"runtime"
)

func main() {
	fmt.Println("逻辑 CPU 数:", runtime.NumCPU())
	fmt.Println("GOMAXPROCS:", runtime.GOMAXPROCS(0)) // 传 0 只查询，不修改
	fmt.Println("当前 goroutine 数:", runtime.NumGoroutine())

	old := runtime.GOMAXPROCS(2) // 设为 2，并返回设置前的旧值
	fmt.Println("设置前的旧值:", old)
	fmt.Println("设置后:", runtime.GOMAXPROCS(0))
	runtime.GOMAXPROCS(old) // 恢复原值，避免影响后续代码
}
```

<OutputBox title="运行结果（具体数字随机器而变）" output="逻辑 CPU 数: 12\nGOMAXPROCS: 12\n当前 goroutine 数: 1\n设置前的旧值: 12\n设置后: 2" note="在一台 12 逻辑核机器上的输出；换台机器前两行的数字会变，但「查询得到默认值、设置后生效、旧值被返回」的结构一致。" />

<LineByLine code="fmt.Println(runtime.NumCPU())\nfmt.Println(runtime.GOMAXPROCS(0))\nold := runtime.GOMAXPROCS(2)\ndefer runtime.GOMAXPROCS(old)\nfmt.Println(runtime.GOMAXPROCS(0))" :notes='["NumCPU 返回机器可用的逻辑 CPU 数，是默认 GOMAXPROCS 的参考","GOMAXPROCS(0) 是查询惯例：只返回当前 P 数，不修改配置","把 P 数设为 2，函数同时返回旧值，方便稍后恢复","用 defer 恢复，避免一次临时调整影响进程里其它代码","再次查询确认生效；P 的数量就是同一时刻能并行执行 Go 代码的上限"]' title="查询与设置 P 的数量" />

P 的数量到底影响什么？用两个纯计算任务测一下就清楚了：同一段代码，只改 P 数，其余不变。

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

// burn 做一段纯计算，会持续占住一个 P
func burn(wg *sync.WaitGroup) {
	defer wg.Done()
	sum := 0
	for i := 0; i < 50000000; i++ {
		sum += i
	}
	_ = sum
}

func run(n int) time.Duration {
	runtime.GOMAXPROCS(n)
	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(2)
	go burn(&wg)
	go burn(&wg)
	wg.Wait()
	return time.Since(start).Round(time.Millisecond)
}

func main() {
	fmt.Println("GOMAXPROCS=1 耗时:", run(1))
	fmt.Println("GOMAXPROCS=2 耗时:", run(2))
}
```

输出形如 `GOMAXPROCS=1 耗时: 42ms` 与 `GOMAXPROCS=2 耗时: 22ms`（数值随机器波动）。两个任务都要吃满 CPU，1 个 P 时只能轮流跑、耗时约等于两者之和；2 个 P 时真正并行，耗时接近单个任务。这直接验证了 P 决定的是**并行度**，而不是能创建多少个 G。

## 3. 深入一步

那是不是「1 个 P 就只能服务 1 个 G」？完全不是。下面让 5 个 goroutine 各自等待 20 毫秒，全程只给 1 个 P。

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

func main() {
	runtime.GOMAXPROCS(1) // 全程只有 1 个 P

	var wg sync.WaitGroup
	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			time.Sleep(20 * time.Millisecond) // 模拟阻塞等待：G 挂起，P 被让出
			fmt.Printf("任务 %d 在唯一的一个 P 上被轮流执行\n", id)
		}(i)
	}
	wg.Wait()
	fmt.Println("5 个 G 全部完成，而 P 始终只有 1 个")
}
```

输出是五行任务信息（顺序不固定）加上最后一行 `5 个 G 全部完成，而 P 始终只有 1 个`。原因很关键：**正在等待的 G 不占 P**。它挂到定时器或等待队列上，M 立刻拿着 P 去跑下一个就绪的 G。所以少量 P 就能撑起大量并发 I/O——P 是「干活的名额」，不是「排队的座位」。

| 实体 | 本质 | 数量由谁决定 | 核心职责 |
| --- | --- | --- | --- |
| G（goroutine） | 一段栈、寄存器现场与状态 | 用户代码，理论无上限 | 承载待执行的用户函数，是最小调度单位 |
| M（Machine） | 操作系统线程，真正执行机器码 | 运行时按需创建，可多于 P | 必须绑定一个 P 才能运行 Go 代码 |
| P（Processor） | 逻辑处理器，一套调度上下文 | `GOMAXPROCS` | 持有本地队列、runnext、mcache 等资源 |

| 维度 | 本地运行队列（P 私有） | 全局运行队列 |
| --- | --- | --- |
| 归属 | 每个 P 各有一条 | 全进程一条，访问需加锁 |
| 容量 | 固定 256 个 G | 无硬上限 |
| 访问成本 | 无锁、最快，P 优先从这里取 | 需争全局锁，是兜底路径 |
| 典型来源 | 当前 P 新建或被唤醒的 G | 本地队列溢出的一半、从系统调用返回的 G |
| 公平性 | 若只顾本地可能饿死别人 | 调度器周期性照顾它，避免饥饿 |

<Term word="调度上下文" text="P 上挂载的一整套调度资源：本地运行队列、runnext 优先槽、mcache 与 defer 池等。M 只有切到某个 P，才能无锁地取 G 并分配内存。" />

<Callout type="warn" title="两个最常见的误读" text="其一，P 不是 CPU 核，也不是线程数上限；M 的数量可以多于 P，多出来的 M 通常在等系统调用或处于空闲。其二，GOMAXPROCS 不限制 goroutine 数量，只限制同一时刻并行执行用户代码的 P 数。把这两点搞反，调优就会南辕北辙。" />

本地队列 256 个槽位是设计取舍：定长数组让入队出队成为无锁的 O(1) 操作；一旦放满，运行时会把其中一半搬到全局队列，既保住了本地访问的快，又给均衡留了出口。至于「空闲 P 怎么去别的 P 偷 G」「M 阻塞时 P 怎么转交」，属于调度循环的动作，下一讲专门拆开。

## 4. 工程实践

绝大多数服务**不需要手动设置 GOMAXPROCS**：默认取逻辑 CPU 数，对 CPU 密集任务已是最优解，对 I/O 密集任务也不构成瓶颈。需要显式干预的通常是容器场景——若默认值没识别出 cgroup 的 CPU 限额，程序会按宿主机核数起太多 P，带来多余的上下文切换与 GC 开销（Go 1.25 起运行时已会参考容器限额）。

<Compare title="GOMAXPROCS 的两种调法" :panels='[{"label":"盲目调大","code":"runtime.GOMAXPROCS(runtime.NumCPU() * 4)\n// 以为 P 越多越快","note":"更多 P 意味着更多 mcache、更多 GC 辅助线程与更频繁的切换；CPU 密集任务通常反而变慢。"},{"label":"保持默认","code":"// 不设置，用运行时默认值\n// 压测确认瓶颈后再按 CPU 限额调整","note":"先用 benchmark 与 pprof 找到真实瓶颈，把调整当作有依据的优化，而不是拍脑袋的开关。"}]' note="调 P 之前先回答：瓶颈是 CPU 不够，还是等待没重叠？后者与 P 数量基本无关。" />

排障时可以打开调度器轨迹，观察 P 数与各队列长度：

```bash
GODEBUG=schedtrace=1000 go run main.go
```

输出里 `gomaxprocs` 是当前 P 数，`idleprocs` 是空闲 P 数，`runqueue` 是全局队列长度。若长期看到大量 `idleprocs` 而请求延迟仍高，说明瓶颈在锁、I/O 或下游，而不是 P 不够——这时调大 GOMAXPROCS 只会掩盖问题。

## 动手练一练

<Exercise title="写一份调度环境自检" task="写一个程序，依次打印 NumCPU、GOMAXPROCS(0)、NumGoroutine；随后把 GOMAXPROCS 设为 2 并确认生效，最后用 defer 恢复原值。要求修改前后都能看到 P 数。" :hints='["runtime.GOMAXPROCS(0) 只查询不修改，是标准写法","设置函数会返回旧值，先存下来再恢复","把恢复写成 defer，保证函数任何退出路径都能还原"]' solution='package main\n\nimport (\n    "fmt"\n    "runtime"\n)\n\nfunc main() {\n    fmt.Println("CPU:", runtime.NumCPU())\n    fmt.Println("P(前):", runtime.GOMAXPROCS(0))\n    fmt.Println("G:", runtime.NumGoroutine())\n    old := runtime.GOMAXPROCS(2)\n    defer runtime.GOMAXPROCS(old)\n    fmt.Println("P(后):", runtime.GOMAXPROCS(0))\n}' explain="GOMAXPROCS(n) 返回设置前的值，因此 old 可以直接交给 defer 复原。注意它影响的是整个进程，库代码里一般不要随手改，交给程序入口统一决定。" />

## 随堂检测

<Quiz no="40-1" q="一个 M 想要执行 Go 代码，必须先具备下列哪个条件？" :options='["先拿到一个可用的 G","先绑定一个 P","先获得全局队列的锁","先向 netpoller 注册"]' answer="1" explain="M 是操作系统线程，但只有持有 P 才拥有本地队列、mcache 等调度资源，才能进入调度循环取 G 执行。G 是它要取的对象，不是前提条件。" />

<Predict no="40-2" q="把 GOMAXPROCS 设为 1，启动 5 个各自只做 time.Sleep(20ms) 的 goroutine，它们能全部跑完吗？" :options='["能，总耗时约等于一次等待的时间","不能，只有一个 P 会死锁","只会跑完 1 个，其余 4 个被丢弃","必须把 GOMAXPROCS 设成 5 才能跑完"]' answer="0" output="5 个 G 全部完成，而 P 始终只有 1 个" explain="等待中的 G 会挂起并让出 P，M 立刻拿这个 P 去跑别的就绪 G。P 是「同时干活的名额」，不是「排队的座位」，所以一个 P 就能服务大量 I/O 型并发。" />

## 一页纸速记

<KnowledgeCard :points='["G 是工单、M 是干活的线程、P 是带本地队列与缓存的工位；M 必须持有 P 才能执行 G","本地队列无锁且优先，容量 256；全局队列加锁，负责溢出、兜底与均衡","P 的数量等于 GOMAXPROCS，限制并行度而非 G 的数量；等待中的 G 不占 P","P 的引入把调度从全局锁竞争变成多数情况下的本地无锁操作"]' :pitfalls='["把 P 当成 CPU 核或线程数上限，或把 GOMAXPROCS 当成 goroutine 上限","不压测就盲目调大 P，增加切换与 GC 开销","误以为 1 个 P 只能服务 1 个 G，从而不敢用 goroutine 承接 I/O"]' summary="一句话记住模型：G 排队、P 发牌、M 干活，三者绑定时才真正执行用户代码。" />

## 小结与下一讲

- G、M、P 分别是执行单元、操作系统线程、调度上下文；M 必须持有 P 才能运行 G。
- 本地队列无锁优先、全局队列加锁兜底，这套分层是 Go 1.1 引入 P 后消除全局锁竞争的关键。
- `GOMAXPROCS` 决定并行度而非 G 的数量；等待中的 G 会交出 P，所以少量 P 能撑起大量并发 I/O。

结构已经清楚，接下来该问「调度器每一轮到底怎么挑 G、怎么不让任何 P 闲着」了。

[下一讲：调度循环：抢占、窃取与 handoff](/concurrency/scheduler)
