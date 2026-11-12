---
title: 第 42 讲 · channel：语义、缓冲与关闭
description: 第 42 讲 · channel：语义、缓冲与关闭 —— 无缓冲与缓冲、发送接收阻塞规则、关闭与遍历、只发送只接收、nil channel
---

<StageHeader stage="L4 · 并发编程" level="★★★☆☆" time="40 分钟" goal="用 channel 正确传递数据与信号" prereq="第 41 讲 · 调度循环：抢占、窃取与 handoff" />

# 第 42 讲 · channel：语义、缓冲与关闭

> 调度器解决了「让 goroutine 都跑起来」，但跑起来之后它们怎么交换数据？共享变量加锁当然可以，Go 更推荐的答案是 channel。它把「传递数据」和「同步时机」合成了一件事：一次发送既是交出值，也是一次握手。这一讲把 channel 的行为规则讲死——什么时候阻塞、什么时候 panic、谁来关闭、关闭后还能读什么。这些规则记住了，后面写 pipeline、select、context 才不会处处踩坑。

## 这一讲你会得到什么

<CheckList id="L42-goal" title="本讲目标" :items='["区分无缓冲与缓冲 channel 的交接语义与适用场景","记牢发送、接收、关闭在 nil、满、空、已关闭时的行为","养成由唯一发送方关闭、用 range 消费的习惯","会用只发送与只接收类型表达意图，并认识 nil channel 陷阱"]' />

## 1. 先建立直觉

用接力赛和快递柜来区分两种 channel。**无缓冲 channel** 是接力赛里的交棒：跑者手里拿着棒，必须等下一棒的人伸手接稳，他才能松手继续跑。发送方会一直阻塞到接收方就绪，两边在交接那一刻同时完成，所以它也叫同步 channel——「发送成功」本身就意味着「对方已经拿到了」。**缓冲 channel** 则是快递柜：柜子有空位，快递员放下就走；柜子满了，他得等有人取走一个格子；柜子空了，取件人得等有人投进来。双方不必同时到场，代价是「发送返回」只保证东西进了柜子，不保证已经被处理。

由此能推出 channel 最有用的性质：它天然带**背压**。生产者跑太快，缓冲满了自然被挡住，不会像无界队列那样把内存吃穿。而 `close` 是这套协议里的结束信号——由发送方宣布「不会再有了」，消费者读到信号后就能干净收尾，不必约定一个特殊值当哨兵。

<ConceptDiagram type="pipeline" title="channel 把生产、处理、消费串成一条流水线" caption="每个箭头都是一次交接：既是数据流动，也是同步点" />

Go 把这套规则做进了类型系统：`chan T` 是双向，`chan<- T`（只发送）与 `<-chan T`（只接收）两种变体可以在函数签名里把「谁只能写、谁只能读」写死，由编译器替你检查。这比靠注释约定可靠得多。

## 2. 语法与规则

先验证无缓冲 channel 的「同步交接」。故意让接收方晚到 100 毫秒，发送方会老老实实等在那里。

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	ch := make(chan string) // 无缓冲：必须收发双方同时就绪
	go func() {
		fmt.Println("发送方: 准备发送")
		ch <- "hello" // 阻塞，直到有人接收
		fmt.Println("发送方: 对方已接走，我才继续")
	}()
	time.Sleep(100 * time.Millisecond) // 故意让接收方晚到
	fmt.Println("接收方: 开始接收")
	msg := <-ch
	fmt.Println("接收方: 收到", msg)
	time.Sleep(10 * time.Millisecond) // 给发送方打印收尾的机会
}
```

<OutputBox title="运行结果（最后两行顺序可能互换）" output="发送方: 准备发送\n接收方: 开始接收\n接收方: 收到 hello\n发送方: 对方已接走，我才继续" note="「准备发送」一定在「开始接收」之前，因为接收方被故意延后了 100ms；交接完成后双方被同时唤醒，故末两行先后不固定。" />

<LineByLine code="ch := make(chan int, 3)\ngo produce(ch)\nfor v := range ch {\n    fmt.Println(v)\n}" :notes='["创建带 3 个槽位的缓冲 channel，最多可暂存 3 个元素","生产者 goroutine 负责填入数据，并在结束时关闭 channel","range 持续接收，直到 channel 关闭且缓冲被取空才退出","循环体拿到的每个值，都是一次独立完成的交接"]' title="缓冲 channel 的标准消费骨架" />

第二段代码覆盖缓冲、关闭、遍历与「再收一次」四个动作，输出完全确定。

```go
package main

import "fmt"

func main() {
	ch := make(chan int, 3) // 带 3 个槽位的缓冲 channel
	ch <- 10
	ch <- 20
	ch <- 30
	fmt.Println("len =", len(ch), "cap =", cap(ch)) // len 是当前排队元素数
	close(ch)                                       // 发送方关闭，缓冲里的数据仍在

	// range 会一直读到 channel 关闭且缓冲排空
	for v := range ch {
		fmt.Println("取出:", v)
	}
	v, ok := <-ch // 已关闭：立即返回零值与 false
	fmt.Printf("再次接收: v=%d ok=%v\n", v, ok)
}
```

输出依次为 `len = 3 cap = 3`、三行 `取出: 10 / 20 / 30`，最后是 `再次接收: v=0 ok=false`。两个结论要记住：**关闭不会丢掉缓冲里已排队的数据**；**已关闭的 channel 仍可无限次接收**，只是每次都立刻拿到元素零值和 `ok=false`。

## 3. 深入一步

第三段代码把单向类型、nil channel 与重复关闭的 panic 放在一个程序里演示。

```go
package main

import "fmt"

// produce 只负责发送，签名把意图写死在类型里
func produce(out chan<- int) {
	for i := 1; i <= 3; i++ {
		out <- i
	}
	close(out) // 唯一的发送方负责关闭
}

// consume 只负责接收
func consume(in <-chan int) {
	for v := range in {
		fmt.Println("消费:", v)
	}
}

func main() {
	ch := make(chan int)
	go produce(ch)
	consume(ch)

	// nil channel：零值 channel 上的收发会永久阻塞
	var nilCh chan int
	fmt.Println("nilCh == nil ?", nilCh == nil)

	// 重复关闭会 panic，这里用 recover 演示（真实代码不要靠 recover）
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("捕获到 panic:", r)
		}
	}()
	close(ch) // ch 已经在 produce 里被关闭
}
```

输出为 `消费: 1`、`消费: 2`、`消费: 3`、`nilCh == nil ? true`、`捕获到 panic: close of closed channel`。双向 `chan int` 可以隐式转成 `chan<- int` 或 `<-chan int`，反过来不行；而忘写 `make` 得到的是零值 nil channel，它既不 panic 也不报错，只是让收发双方永远睡下去——这是最难查的一类 bug。

| 操作 | nil channel | 无缓冲且无配对 | 缓冲有空位 / 有数据 | 缓冲满 / 空 | 已关闭 |
| --- | --- | --- | --- | --- | --- |
| 发送 | 永久阻塞 | 阻塞到接收者就绪 | 立即成功 | 满则阻塞 | **panic** |
| 接收 | 永久阻塞 | 阻塞到发送者就绪 | 立即成功 | 空则阻塞 | 先取完剩余数据，之后立刻返回零值且 `ok=false` |
| 关闭 | **panic** | 成功 | 成功 | 成功 | **panic** |
| `len` / `cap` | `0` / `0` | 返回排队数与容量 | 同左 | 同左 | 同左 |

| 维度 | 无缓冲 channel | 缓冲 channel |
| --- | --- | --- |
| 交接方式 | 同步交接，收发必须同时就绪 | 异步暂存，允许双方错峰 |
| 发送返回的含义 | 接收方已经把值接走 | 值已入队，不保证被处理 |
| 背压 | 天然存在，节奏完全耦合 | 由容量决定，容量即允许积压量 |
| 典型用途 | 信号通知、请求应答、强同步 | 削峰填谷、解耦生产与消费速率 |

<Term word="comma-ok" text="接收表达式的第二个返回值：为 false 时表示 channel 已关闭且缓冲已排空，此时拿到的值是元素类型的零值，用它区分「真的收到一个零值」与「已经收完了」。" />

<Callout type="warn" title="四条最容易踩的红线" text="向已关闭的 channel 发送会 panic；关闭已关闭的 channel 会 panic；关闭 nil channel 会 panic；在 nil channel 上收发会永久阻塞。前三条靠约定防住——关闭只能由唯一发送方做一次，绝不要靠 recover 兜底；第四条靠初始化检查防住。" />

## 4. 工程实践

关闭协议的第一条纪律：**谁发送，谁关闭**。若只有一个生产者（含用 `go` 启动的那一个），它填完数据后直接 `close` 即可，消费者用 `for range` 收尾，代码最干净。若有多个生产者，任何一家提前关闭都会让其它家撞上「向已关闭 channel 发送」的 panic；这时要引入额外的完成信号，等所有生产者都收工后再由一个协调者统一关闭——具体做法会在 sync 与 context 相关讲次展开，本讲先记住「不要在消费者里关闭」这条底线。

<Compare title="无缓冲与缓冲怎么选" :panels='[{"label":"无缓冲","code":"ch := make(chan int)\n// 强同步，收发必须同时就绪","note":"发送返回即代表对方已接走，适合信号、请求应答与严格的阶段同步；代价是双方节奏完全耦合。"},{"label":"缓冲","code":"ch := make(chan int, 64)\n// 允许突发，削峰填谷","note":"生产者不必等消费者，能吸收短时峰值；但发送成功不等于处理完成，容量拍太大就失去背压。"}]' note="容量不是越大越好：它决定你愿意为「生产快于消费」垫付多少内存与延迟。" />

还要克制一个误解：channel 不是消息队列。它没有持久化、没有重试、没有确认机制，进程一退出数据就没了；它只是**同一进程内** goroutine 之间的同步交接原语。跨服务、要可靠投递的场景该用真正的 MQ。另外别为了「传个信号」去造带数据的 channel，`chan struct{}` 更省内存、意图也更清楚。

## 动手练一练

<Exercise title="验证关闭后 range 能干净收尾" task="创建一个容量为 2 的缓冲 channel，不做任何发送就立刻关闭，然后用 for range 遍历它，并在循环后打印一行提示。观察循环是正常退出还是永久阻塞，并解释原因。" :hints='["close 可以在没有任何发送的情况下调用","range 在 channel 关闭且缓冲排空时会自然结束","循环之后打印一行文字，就能证明程序没有卡住"]' solution='package main\n\nimport "fmt"\n\nfunc main() {\n    ch := make(chan int, 2)\n    close(ch)\n    for v := range ch {\n        fmt.Println(v)\n    }\n    fmt.Println("遍历结束")\n}' explain="range 的终止条件是「channel 已关闭且缓冲已排空」，与是否发送过数据无关。因此循环体一次都不执行，程序直接打印「遍历结束」，不会阻塞也不会 panic。" />

## 随堂检测

<Quiz no="42-1" q="向一个已经关闭的 channel 发送数据，会发生什么？" :options='["立即返回元素零值","触发 panic","永久阻塞","数据被静默丢弃"]' answer="1" explain="关闭意味着「不会再有数据了」，此时再发送属于违反协议，运行时直接 panic。接收方向则相反：关闭后仍可接收剩余数据，排空后无限次返回零值与 ok=false。" />

<Predict no="42-2" q="容量为 2 的缓冲 channel 里放入 10、20 后关闭，再用 for range 遍历，最后再接收一次，会看到什么？" :options='["遍历输出 10 20，最后一次拿到 0 且 ok=false","遍历输出 10 20，最后一次永久阻塞","遍历直接 panic，因为 channel 已关闭","遍历只输出 10，20 随关闭被丢弃"]' answer="0" output="取出: 10\n取出: 20\n再次接收: v=0 ok=false" explain="关闭不会丢弃缓冲里已排队的数据，range 会先把它们取空再退出。此后继续接收会立刻返回零值与 ok=false，既不阻塞也不 panic。" />

## 一页纸速记

<KnowledgeCard :points='["无缓冲是同步交接，发送返回即代表接收完成；缓冲是异步暂存，容量即允许积压量","关闭由唯一发送方负责，range 在「已关闭且排空」时自然退出","已关闭 channel 可继续接收：剩余数据照常取，之后返回零值加 ok=false","只发送与只接收类型把意图写进签名，由编译器检查"]' :pitfalls='["向已关闭 channel 发送、重复关闭、关闭 nil channel 都会 panic","忘记 make 得到 nil channel，收发永久阻塞且毫无报错","在消费者里关闭 channel，或让多个生产者各自关闭","把 channel 当持久化消息队列，或把缓冲容量拍得过大而丢掉背压"]' summary="channel 的规则可以浓缩成一句：发送方负责关闭，接收方负责收尾，关闭之后只读不写。" />

## 小结与下一讲

- 无缓冲 channel 是同步交接，缓冲 channel 允许错峰；`len` 是排队元素数，容量决定背压强度。
- 关闭由唯一发送方完成，`for range` 是最稳的消费方式；关闭后数据仍可读，排空后返回零值与 `ok=false`。
- 三种 panic、一种永久阻塞是高频错误源；单向类型与「谁发送谁关闭」的约定能防住大部分。

规则记牢了，下一讲我们把 channel 拆开，看运行时到底用什么结构把「阻塞的收发双方」配对起来。

[下一讲：channel 底层 hchan 与收发队列](/concurrency/hchan)
