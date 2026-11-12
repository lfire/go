---
title: 第 43 讲 · channel 底层 hchan 与收发队列
description: 第 43 讲 · channel 底层 hchan 与收发队列 —— hchan 结构、sendq/recvq、缓冲区环形队列、直接拷贝与唤醒、select 实现基础
---

<StageHeader stage="L4 · 并发编程" level="★★★★★" time="45 分钟" goal="解释 channel 的行为为何如此" prereq="第 42 讲 · channel：语义、缓冲与关闭" />

# 第 43 讲 · channel 底层 hchan 与收发队列

> 上一讲我们把 channel 的规则背了下来：什么时候阻塞、什么时候 panic、关闭后还能读什么。这一讲回答「为什么」。`make(chan T, n)` 返回的并不是一根管道，而是一个叫 `hchan` 的运行时结构体；那些看似零散的规则，其实都来自同一套设计——**一个带锁的环形缓冲，加两条把收发双方直接配对的等待队列**。看懂它，你就能自己推导出所有边界行为，也能解释高并发下 channel 为什么会有性能拐点。

## 这一讲你会得到什么

<CheckList id="L43-goal" title="本讲目标" :items='["说出 hchan 的关键字段及其作用","理解 sendq / recvq 如何把阻塞的收发双方配对","解释环形缓冲的 sendx / recvx 回绕，以及为何不需要搬移数据","知道直接拷贝与唤醒的过程，并理解 select 的实现基础"]' />

## 1. 先建立直觉

先问一个朴素问题：发送方和接收方几乎总不会同时到达，先到的那个去哪儿了？答案不是「把值先塞进某个地方等一等」这么简单，而是**把先到的 goroutine 本身挂起来登记**。hchan 里有两个等待队列：`recvq` 存「等着收的 G」，`sendq` 存「等着发的 G」，队列节点叫 `sudog`，里面记着 G 的指针以及「值要写到哪里 / 从哪里读」。后到的一方不必再碰缓冲，运行时直接从对面队列队首取出一个 `sudog`，把值**直接拷贝**到对方指定的变量里，然后唤醒那个 G——一次交接完成，双方同时继续。

只有当对面没有等待者时，缓冲才有意义：有空位就把值放进环形槽位，没空位就把当前 G 也包成 `sudog` 挂进自己的队列并 `gopark` 让出 P。所以上一讲「无缓冲 channel 的发送会阻塞到接收者就绪」不是一句约定，而是「没有槽位可放，只能进 sendq 等配对」的必然结果。

<ConceptDiagram type="hchan" title="hchan：一个环形缓冲加两条等待队列" caption="sendq 与 recvq 把收发双方直接配对，buf 只是没人配对时的暂存区" />

整个结构由一把 `lock` 保护：无论发送、接收还是关闭，都要先拿锁再改字段。这也提前埋下一个工程结论——channel 不是无锁结构，极高竞争下这把锁与 `sudog` 的分配都会成为热点。

## 2. 语法与规则

先用一个可运行的程序模拟环形缓冲的核心逻辑：写入位置 `sendx` 与读取位置 `recvx` 各自按容量取模回绕，因此缓冲区满了之后继续写入，覆盖的是已经取走的旧槽位，永远不需要把整个数组搬来搬去。

```go
package main

import "fmt"

// ring 用与 hchan 相同的思路模拟一个有界环形缓冲
type ring struct {
	buf      []int
	dataqsiz int
	qcount   int
	sendx    int // 下一个写入位置
	recvx    int // 下一个读取位置
}

func newRing(n int) *ring { return &ring{buf: make([]int, n), dataqsiz: n} }

func (r *ring) send(v int) bool {
	if r.qcount == r.dataqsiz {
		return false // 满：真实 hchan 会把发送者挂进 sendq
	}
	r.buf[r.sendx] = v
	r.sendx = (r.sendx + 1) % r.dataqsiz // 回绕，避免搬移数据
	r.qcount++
	return true
}

func (r *ring) recv() (int, bool) {
	if r.qcount == 0 {
		return 0, false // 空：真实 hchan 会把接收者挂进 recvq
	}
	v := r.buf[r.recvx]
	r.recvx = (r.recvx + 1) % r.dataqsiz
	r.qcount--
	return v, true
}

func main() {
	r := newRing(3)
	for _, v := range []int{10, 20, 30} {
		fmt.Println("发送", v, "成功:", r.send(v))
	}
	fmt.Println("缓冲区满后再发送:", r.send(40))
	for i := 0; i < 3; i++ {
		v, _ := r.recv()
		fmt.Println("接收:", v)
	}
	fmt.Println("sendx =", r.sendx, "recvx =", r.recvx, "qcount =", r.qcount)
}
```

<OutputBox title="运行结果" output="发送 10 成功: true\n发送 20 成功: true\n发送 30 成功: true\n缓冲区满后再发送: false\n接收: 10\n接收: 20\n接收: 30\nsendx = 0 recvx = 0 qcount = 0" note="写入三次后 sendx 回到 0，读取三次后 recvx 也回到 0：环形下标都绕了一圈，元素始终保持先进先出。" />

<LineByLine code="type hchan struct {\n    qcount uint\n    dataqsiz uint\n    sendx uint\n    recvx uint\n    recvq waitq\n    sendq waitq\n}" :notes='["hchan 是 channel 在运行时的真实载体，make 返回的是指向它的指针","qcount 记录缓冲区当前排队的元素个数，对应内建 len","dataqsiz 是环形缓冲槽位数，对应内建 cap；无缓冲时为 0","sendx 是下一个写入位置，按环形下标回绕前进","recvx 是下一个读取位置，与 sendx 配合避免搬移数据","recvq 是阻塞接收者的等待队列，节点是一个个 sudog","sendq 是阻塞发送者的等待队列，与 recvq 一起完成配对","真实结构还有 buf、closed、elemtype 与 lock，这里没有一一列出"]' title="hchan 结构速览" />

再验证「直接拷贝」。无缓冲 channel 没有 `buf`，值是在交接那一刻从发送方的栈拷到接收方的变量里的，之后两边各不相干。

```go
package main

import "fmt"

type payload struct{ n int }

func main() {
	ch := make(chan payload) // 无缓冲
	done := make(chan struct{})
	go func() {
		p := payload{n: 7}
		ch <- p       // 运行时把 p 直接拷贝到接收方的变量上
		p.n = 999     // 交接后再改发送方本地变量，不影响已交出去的值
		fmt.Println("发送方修改后:", p.n)
		close(done)
	}()
	got := <-ch
	<-done // 等发送方收尾，保证输出顺序
	fmt.Println("接收方拿到:", got.n)
}
```

输出为 `发送方修改后: 999` 与 `接收方拿到: 7`。发送方把本地变量改成 999，接收方仍然拿到 7，说明交接是一次**值拷贝**而非共享引用。也正因如此，channel 传递的是值语义：想传大对象又不想拷贝，只能传指针，而指针就把所有权与逃逸问题一起带回来了。

## 3. 深入一步

下面让三个 goroutine 同时在无缓冲 channel 上接收，它们会依次挂进 `recvq`；随后三次发送，每次唤醒队首的一个 `sudog`。

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	ch := make(chan int) // 无缓冲：没有槽位，接收者只能排进 recvq
	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			v := <-ch // 三个 G 都挂起，登记进 recvq
			fmt.Printf("接收者 %d 被唤醒，拿到 %d\n", id, v)
		}(i)
	}
	time.Sleep(10 * time.Millisecond) // 确保三个接收者都已入队
	fmt.Println("此时 recvq 里排着 3 个等待的 G")
	ch <- 100 // 每次发送从 recvq 队首取一个 sudog，直接拷贝并唤醒
	ch <- 200
	ch <- 300
	wg.Wait()
	fmt.Println("全部接收者完成")
}
```

<OutputBox title="运行结果（接收者编号与数值的对应不固定）" output="此时 recvq 里排着 3 个等待的 G\n接收者 2 被唤醒，拿到 300\n接收者 3 被唤醒，拿到 200\n接收者 1 被唤醒，拿到 100\n全部接收者完成" note="三个值各被一个接收者拿到，三条信息都会打印；但哪个编号拿到哪个值、以及打印的先后，都由入队与调度顺序决定。" />

<StepLab scene="channel-flow" />

| 字段 | 含义 | 作用 |
| --- | --- | --- |
| `qcount` | 当前排队元素数 | 对应 `len(ch)`，判断缓冲空满 |
| `dataqsiz` | 环形缓冲槽位数 | 对应 `cap(ch)`，无缓冲时为 0 |
| `buf` | 指向元素数组的指针 | 按 `elemsize` 定长步进，无缓冲时为空 |
| `elemsize` / `elemtype` | 元素大小与类型 | 决定一次拷贝多少字节、如何写屏障 |
| `sendx` / `recvx` | 写入与读取下标 | 到达末尾回绕到 0，实现先进先出 |
| `recvq` / `sendq` | `sudog` 链表 | 分别挂起阻塞的接收者与发送者 |
| `closed` | 关闭标志 | 发送前检查，已关闭则 panic |
| `lock` | 互斥锁 | 保护以上所有字段，收发都要先拿锁 |

| 情形 | `chansend` / `chanrecv` 的动作 |
| --- | --- |
| 有等待的接收者 | 直接把值拷进该 `sudog` 指定的接收变量，`goready` 唤醒它，不经过缓冲 |
| 缓冲未满 | 拷到 `buf[sendx]`，`sendx` 回绕，`qcount` 加一 |
| 缓冲满 / 无缓冲且无人等 | 把当前 G 包成 `sudog` 挂进 `sendq`，`gopark` 让出 P |
| 接收时缓冲已满且 `sendq` 有人等 | 先取走 `recvx` 处的值，再把等待发送者的值放到队尾槽位并唤醒它 |
| 已关闭 | 发送 panic；接收先取尽剩余数据，之后返回零值与 `ok=false` |
| nil channel | 直接 `gopark`，没有任何路径能把它唤醒，永久阻塞 |

<Term word="sudog" text="运行时表示「一个等待在某条 channel 上的 G」的结构：记录 G 指针与待收发值的地址，是 recvq 与 sendq 链表的节点；select 也会为每个 case 准备一个。" />

<Callout type="warn" title="别把 channel 当无锁队列" text="每次收发都要先拿 hchan.lock，还要在阻塞路径上分配 sudog 并做 gopark / goready。高并发竞争下这把锁是明确的热点，元素越大拷贝成本也越高。纯计数场景用 atomic、保护小临界区用 Mutex，往往比绕一圈 channel 更合适。" />

这也解释了 `select` 的实现基础：运行时会把当前 G 的 `sudog` **同时挂到所有 case 对应 channel 的等待队列**上，哪个 channel 先就绪，就由对方把这一个 `sudog` 摘走并唤醒 G，其余队列再把自己的那份注销掉。所谓「随机选一个就绪 case」「都不就绪就阻塞」，都建立在这套登记与唤醒机制之上；语法层面的细节留到下一讲。

## 4. 工程实践

元素大小直接影响成本。hchan 的 `buf` 是一段按 `elemsize` 步进的连续内存，每收发一个元素都要做一次完整拷贝，还会触发写屏障。传一个 1 KB 的结构体，和传一个指向它的指针，代价差了两个数量级。

<Compare title="传值还是传指针" :panels='[{"label":"传值","code":"type Event struct {\n    Payload [1024]byte\n}\nch := make(chan Event, 16)","note":"每个槽位 1KB，每次收发整块拷贝；值语义清晰，发送后随便改本地变量，但大对象很贵。"},{"label":"传指针","code":"type Event struct {\n    Payload [1024]byte\n}\nch := make(chan *Event, 16)","note":"只拷 8 字节指针，成本极低；但发送后不得再修改对象，且对象会逃逸到堆、引入共享与 GC 压力。"}]' note="经验法则：小值类型直接传值；大对象先想清楚所有权再传指针，并且约定「交出去就不再碰」。" />

源码层面，本讲对应的实现都在 `runtime/chan.go`：`makechan` 分配 hchan 与 `buf`，`chansend` 与 `chanrecv` 实现上面的路径表，`closechan` 负责唤醒所有等待者并置 `closed`，`selectgo` 处理多路复用。读这段代码时抓住一条主线就够了——**先尝试和对面队列配对，配对不上才用缓冲，缓冲也用不上就挂起自己**。

缓冲容量同样要按这个模型来定：它决定「生产快于消费」时愿意垫付多少内存与延迟，而不是越大越好。若长期看到 `sendq` 堆积（`go tool pprof` 的 goroutine profile 里全是 `chansend`），说明背压已经在了，该加消费者或降低生产速率，而不是继续调大容量。

## 动手练一练

<Exercise title="用取模实现环形下标回绕" task="写一个小程序：设环形缓冲容量为 3，用取模让 sendx 连续回绕打印 5 次写入位置，再让 recvx 回绕打印 3 次读取位置。观察下标是否按 0、1、2、0、1 的规律前进。" :hints='["下一个位置写成 (sendx + 1) % n","sendx 与 recvx 各自独立回绕","循环条件用不等于比较，避免多余符号"]' solution='package main\n\nimport "fmt"\n\nfunc main() {\n    const n = 3\n    sendx, recvx := 0, 0\n    for i := 0; i != 5; i++ {\n        fmt.Println("send at", sendx)\n        sendx = (sendx + 1) % n\n    }\n    for i := 0; i != 3; i++ {\n        fmt.Println("recv at", recvx)\n        recvx = (recvx + 1) % n\n    }\n}' explain="输出依次是 send at 0、1、2、0、1 与 recv at 0、1、2。取模让下标到末尾自动回到 0，这正是 hchan 的 sendx 与 recvx 能原地复用槽位、不必搬移整个数组的原因。" />

## 随堂检测

<Quiz no="43-1" q="无缓冲 channel 上，发送方到达时已有一个接收者等在 recvq 里，运行时会怎么做？" :options='["把值放进一块临时缓冲区，稍后再交给接收者","直接把值拷贝到该 sudog 指定的接收变量并唤醒它","丢弃这个值并返回，让接收者拿到零值","为这个 channel 新建一个槽位"]' answer="1" explain="有等待的接收者时走直接拷贝路径：值从发送方栈拷到接收方变量，同时 goready 唤醒接收者，完全不经过 buf。这也是无缓冲 channel 能做到同步交接的原因。" />

<Predict no="43-2" q="三个 goroutine 都在同一个无缓冲 channel 上接收，随后依次发送 100、200、300，会看到什么？" :options='["三个接收者全部阻塞，发送方也卡住","三个接收者各被唤醒一次，三条信息都打印","只有第一个接收者被唤醒，其余两个永久泄漏","编译通过但运行时 panic"]' answer="1" output="此时 recvq 里排着 3 个等待的 G\n接收者 2 被唤醒，拿到 300\n接收者 3 被唤醒，拿到 200\n接收者 1 被唤醒，拿到 100\n全部接收者完成" explain="三次发送分别配对 recvq 队首的一个 sudog，逐个唤醒，因此三条信息都会打印且三个值各被取走一次。但哪个编号拿到哪个值、以及打印先后，都由入队与调度顺序决定。" />

## 一页纸速记

<KnowledgeCard :points='["hchan = 环形缓冲 buf + sendq/recvq 两条等待队列 + closed 标志 + 一把 lock","有对面等待者就直接拷贝并唤醒，配对不上才用缓冲，缓冲也用不上就 gopark 挂起自己","sendx / recvx 取模回绕，保证先进先出且不需要搬移数据","select 把同一个 G 的 sudog 挂到所有 case 的队列上，谁先就绪谁唤醒它"]' :pitfalls='["以为 channel 是无锁的，忽略 hchan.lock 在高竞争下的热点","传大结构体按值收发，拷贝与写屏障成本被放大","传指针却在发送后继续改对象，重新引入共享与数据竞争","把缓冲容量当性能开关，无视 sendq 堆积发出的背压信号"]' summary="channel 的全部行为都源于一件事：先配对队列，再退而用环形缓冲，最后才挂起自己。" />

## 小结与下一讲

- `make(chan T, n)` 背后是 hchan：`buf` 环形缓冲、`sendq`/`recvq` 等待队列、`closed` 标志与保护它们的 `lock`。
- 收发优先与对面队列的 `sudog` 直接拷贝并唤醒；配对不上才走缓冲，缓冲不可用就 `gopark` 挂起，这解释了全部阻塞与 panic 规则。
- 大元素按值传递拷贝昂贵，指针虽省拷贝却带来所有权与逃逸问题；`select` 的实现基础就是把一个 G 的 `sudog` 登记到多个队列。

既然一个 G 可以同时等在多条 channel 上，下一讲就把这个能力变成语法：select、超时与多路复用。

[下一讲：select、超时与多路复用](/concurrency/select)
