---
title: 第 043 讲 · channel 底层 hchan 与收发队列
description: 第 043 讲 · channel 底层 hchan 与收发队列 —— hchan 结构、sendq/recvq、缓冲区环形队列、直接拷贝与唤醒、select 实现基础
---

<StageHeader stage="L4 · 并发编程" level="★★★★★" time="45 分钟" goal="解释 channel 的行为为何如此" prereq="第 042 讲 · channel：语义、缓冲与关闭" />

# 第 043 讲 · channel 底层 hchan 与收发队列

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

### 再深一层：hchan.lock 为何算同步点、直接拷贝的边界与可验证证据

<Callout type="note" title="进阶选读 · 首次学习可跳过" text="本段涉及运行时源码、内存模型与性能工具等后面讲次才系统展开的内容，属于加深理解的可选层。第一次学建议先掌握本讲主线，遇到尚未讲过的术语不必停下，学完对应讲次再回看，不影响后续理解。" />

第 042 讲末尾的进阶段落列过 channel 的四条 happens-before 规则，这一讲要回答它们从哪来。一个常见误解是「因为值被拷贝了，所以同步」——不对。同步边来自 `runtime/chan.go` 里 `chansend` / `chanrecv` / `closechan` 的**锁与唤醒**：`chansend` 先拿 `hchan.lock`，找到 `recvq` 队首的 `sudog` 后由 `sendDirect` 把值直接拷进接收方指定的变量，再 `goready(gp)` 唤醒那个 G；接收方从 `gopark` 返回时重新进入调度。锁的 acquire/release 加上 goroutine 唤醒，恰好实现了 `go_mem.html` 里「发送 synchronized before 对应接收完成」那条规则；`closechan` 依次唤醒 `recvq` 里的全部 `sudog`，对应「关闭 synchronized before 读到零值的接收」。所以**同步来自 hchan.lock 与配对唤醒，不是来自拷贝本身**。

由此能推出一条最容易被忽略的边界：直接拷贝是**值语义**，只拷 `elemsize` 个字节。若通道里传的是指针，拷贝的只是 8 字节地址，指向的堆对象仍被收发双方共享，**channel 不替你保证这个对象不被并发改写**。发送方在 `ch <- e` 之后继续改 `e` 的字段，与接收方的读之间没有任何 happens-before，就是数据竞争。于是「交出去就不再碰」不是风格建议，而是内存模型约束；这也解释了传大对象时的两难——按值传拷贝昂贵，按指针传省 CPU 却把对象推到堆上、引入共享与 GC 压力。

| 机制 | 保证 | 不保证 / 边界 |
| --- | --- | --- |
| `hchan.lock` | 收发与关闭字段的互斥，构成同步边 | 不是无锁结构，高竞争下是明确热点 |
| 直接拷贝值 | 交接那一刻的完整值副本 | 不拷贝指针指向的堆对象 |
| 传 `*T` | 只拷地址，零拷贝大对象 | 不保证对象所有权、不保证无竞争 |
| `sudog` 配对唤醒 | 建立 happens-before | 不保证哪个接收者拿到哪个值 |
| 缓冲槽位复用 | 第 k 次接收与第 k+C 次发送之间有同步 | 不保证已被下游处理完成 |

`-race` 在这里能抓的正是「传指针后继续改对象」这类无同步冲突读写；**抓不到** `sendq` 堆积形成的背压（那是容量与性能问题，没有冲突访问），也**抓不到**「哪个 G 被唤醒」这种由入队与调度顺序决定的逻辑结果。把两种写法做成对照：`goodHandoff` 按值交接全绿且恒为 7，`badHandoff` 传指针后改写，`-race` 精确打印两段冲突栈。

```go
package main

import (
	"os"
	"sync/atomic"
	"testing"
)

type Event struct {
	Payload [1024]byte
}

var sinkByte atomic.Int64

// badHandoff 传指针，发送方在交接后继续改写对象，构成数据竞争
func badHandoff() int {
	ch := make(chan *Event, 1)
	go func() {
		e := &Event{}
		ch <- e
		e.Payload[0] = 9 // 交出去之后还改
	}()
	e := <-ch
	return int(e.Payload[0])
}

// goodHandoff 传值，交接是一次完整拷贝，之后各不相干
func goodHandoff() int {
	ch := make(chan Event, 1)
	go func() {
		var e Event
		e.Payload[0] = 7
		ch <- e
		e.Payload[0] = 9 // 改的是本地副本
	}()
	e := <-ch
	return int(e.Payload[0])
}

func TestHandoffValue(t *testing.T) {
	if got := goodHandoff(); got != 7 {
		t.Fatalf("期望 7，实际 %d", got)
	}
}

// 反面实验：设 SHOW_RACE=1 并配合 -race，暴露传指针后改写的 DATA RACE
func TestPointerHandoffRace(t *testing.T) {
	if os.Getenv("SHOW_RACE") == "" {
		t.Skip("反面实验：设 SHOW_RACE=1 并加 -race 运行")
	}
	_ = badHandoff()
}

func BenchmarkByValue(b *testing.B) {
	ch := make(chan Event, 1)
	for i := 0; i < b.N; i++ {
		ch <- Event{}
		e := <-ch
		sinkByte.Store(int64(e.Payload[0]))
	}
}

func BenchmarkByPointer(b *testing.B) {
	ch := make(chan *Event, 1)
	for i := 0; i < b.N; i++ {
		ch <- &Event{}
		e := <-ch
		sinkByte.Store(int64(e.Payload[0]))
	}
}
```

```bash
go test -race -run=TestHandoffValue -v ./...                  # 传值交接：全绿，恒为 7
SHOW_RACE=1 go test -race -run=TestPointerHandoffRace ./...  # 传指针后改写：报 DATA RACE
go test -bench='ByValue|ByPointer' -benchmem -run=^$ ./...   # 看 ns/op、B/op 与 allocs/op
go test -run=^$ -bench=BenchmarkByValue -mutexprofile=mutex.out ./... && go tool pprof -top -mutex mutex.out
go tool pprof -alloc_objects mem.out                           # 定位谁在分配大对象
```

<OutputBox title="BenchmarkByValue 与 BenchmarkByPointer 本机实测（Go 1.23.8 / darwin-arm64）" output="BenchmarkByValue-12：约 180 ns/op，0 B/op，0 allocs/op\nBenchmarkByPointer-12：约 45 ns/op，1024 B/op，1 allocs/op" note="传值每轮拷 1KB 更费 CPU；传指针省拷贝却每轮分配 1024B 堆对象" />

逐字段读：`ns/op` 上按值传约 180ns、按指针约 45ns，差额就是每轮 1KB 的完整拷贝，印证「元素越大拷贝成本越高」；但 `B/op` 与 `allocs/op` 反过来——按值传 0 B、0 次分配，按指针传 1024 B/op、1 allocs/op，因为 `&Event{}` 让对象逃逸到堆、每轮多一次分配与后续 GC 压力。所以「传指针一定更快」是只看了 CPU 的片面结论，是否值得必须用 `-benchmem` 的分配列一起判断。小值类型直接传值，大对象才在「所有权清晰、交出不再碰」的前提下传指针。

<OutputBox title="go tool pprof -top -mutex 的示意输出" output="Type: delay\n      flat  flat%   sum%        cum   cum%\n     0.58s 96.67% 96.67%      0.58s 96.67%  runtime.chansend\n     0.02s  3.33%   100%      0.60s   100%  main.BenchmarkByValue\nContentions: 12000   Delay: 600ms" note="runtime.chansend 出现在 mutex profile 顶部，是 channel 并非无锁的直接证据" />

`flat` 是该函数自身采样到的等待时长，`flat%` 与 `sum%` 是占比与累计占比，`cum` 是含调用链的累计值，`Contentions` 是争用次数、`Delay` 是累计等待时长。`runtime.chansend` 排在顶部，说明大量时间花在 `hchan.lock` 的获取上——高竞争下这把锁与 `sudog` 的分配都是热点。纯计数场景改用 `atomic`、保护小临界区改用 `Mutex`，通常比绕一圈 channel 更合适。

失败时序落在背压：生产快于消费时，缓冲填满后发送方被包成 `sudog` 挂进 `sendq` 并 `gopark`，直到有接收者取走一个槽位才被唤醒。若下游持续超时、停止消费，`sendq` 会不断堆积，已经入队的结果只被部分处理，形成「部分成功」；这类问题 `-race` 查不到，要靠 goroutine profile 里卡在 `chansend` 的 G 与 `gctrace` 的堆增长来定位。

<SequenceDiagram title="sendq 堆积与背压的形成" caption="缓冲满后发送方挂进 sendq，消费超时导致队列持续堆积" :actors='["生产者","hchan","sendq","消费者","超时器"]' :steps='[{"from":"生产者","to":"hchan","label":"高速发送","note":"速率超过消费","kind":"call"},{"from":"hchan","to":"hchan","label":"写 buf[sendx]","note":"环形回绕","kind":"self"},{"from":"hchan","to":"sendq","label":"缓冲满挂起","note":"包装 sudog gopark","kind":"call"},{"from":"消费者","to":"hchan","label":"取走一个","note":"唤醒队首发送者","kind":"call"},{"from":"hchan","to":"sendq","label":"goready 队首","note":"直接拷贝并唤醒","kind":"return"},{"from":"sendq","to":"生产者","label":"恢复发送","note":"接力继续","kind":"return"},{"from":"超时器","to":"消费者","label":"处理超时","note":"下游变慢","kind":"async"},{"from":"消费者","to":"hchan","label":"停止消费","note":"不再取数据","kind":"call"},{"from":"hchan","to":"sendq","label":"持续堆积","note":"背压与部分成功","kind":"async"}]' />

两条工程误解，一条关于锁、一条关于所有权：

<Compare title="别把 channel 当无锁队列" :panels='[{"label":"错误：热点也绕通道","code":"// 高并发计数也用 channel 绕一圈\nch := make(chan int, 1)\n// 每次读写都要拿 hchan.lock","note":"收发都要先拿 hchan.lock，阻塞路径还要分配 sudog；pprof -mutex 顶部会看到 runtime.chansend。"},{"label":"正确：按场景选原语","code":"var n atomic.Int64\nn.Add(1) // 单条指令，无锁队列开销\n// 或 mu.Lock 护一小段临界区","note":"纯计数用 atomic、保护小临界区用 Mutex，通常比绕一圈 channel 更直接。"}]' note="channel 是同步交接原语，不是无锁数据结构。" />

<Compare title="传指针之后还能不能改" :panels='[{"label":"错误：交出去继续改","code":"e := &Event{}\nch &lt;- e\ne.Payload[0] = 9 // 与接收方的读无同步","note":"channel 只拷了 8 字节地址；交接后的改写与接收方的读之间无 happens-before，-race 会报。"},{"label":"正确：值语义或交出不再碰","code":"var e Event\ne.Payload[0] = 7\nch &lt;- e // 完整拷贝，交出后不碰\n// 或传指针但约定不再修改","note":"按值传得到独立副本；若传指针，必须约定所有权随交接转移。"}]' note="省拷贝的代价是把所有权与共享问题一起带回来。" />

这些结论在实战里都有落点。第 109 讲 [并发爬虫](/projects/concurrent-crawler) 的 `Page` 结构在解析层与落地层之间传递，大对象传指针但必须约定「交出不再碰」，Frontier 有界队列满时把背压传到生产端，正是 `sendq` 堆积的工程版本；第 110 讲 [分布式任务调度](/projects/concurrent-scheduler) 的任务队列带租约与确认，派发器用多路复用在多个队列间取任务；第 116 讲 [秒杀与高并发库存](/projects/micro-seckill) 用有界队列削峰、结果轮询与超时释放，把「缓冲容量即允许积压量」和「读改写必须合成原子步骤」两条一起用上。

<Quiz q="无缓冲 channel 传 *Event 指针，发送方在 ch 发送 e 之后又改 e 的字段，go test -race 会怎样？" :options='["全绿，channel 已经建立同步","报 WARNING: DATA RACE，因为交接后的改写与接收方的读之间没有 happens-before","直接 panic","编译报错"]' answer="1" explain="channel 收发只同步到交接那一刻，拷贝的是 8 字节地址而非对象；发送之后对对象的改写与接收方的读之间无同步边，构成数据竞争。传大对象要么按值传、要么约定交出后不再碰，必要时深拷贝。" />

## 4. 工程实践

元素大小直接影响成本。hchan 的 `buf` 是一段按 `elemsize` 步进的连续内存，每收发一个元素都要做一次完整拷贝，还会触发写屏障。传一个 1 KB 的结构体，和传一个指向它的指针，代价差了两个数量级。

<Compare title="传值还是传指针" :panels='[{"label":"传值","code":"type Event struct {\n    Payload [1024]byte\n}\nch := make(chan Event, 16)","note":"每个槽位 1KB，每次收发整块拷贝；值语义清晰，发送后随便改本地变量，但大对象很贵。"},{"label":"传指针","code":"type Event struct {\n    Payload [1024]byte\n}\nch := make(chan *Event, 16)","note":"只拷 8 字节指针，成本极低；但发送后不得再修改对象，且对象会逃逸到堆、引入共享与 GC 压力。"}]' note="经验法则：小值类型直接传值；大对象先想清楚所有权再传指针，并且约定「交出去就不再碰」。" />

源码层面，本讲对应的实现都在 `runtime/chan.go`：`makechan` 分配 hchan 与 `buf`，`chansend` 与 `chanrecv` 实现上面的路径表，`closechan` 负责唤醒所有等待者并置 `closed`，`selectgo` 处理多路复用。读这段代码时抓住一条主线就够了——**先尝试和对面队列配对，配对不上才用缓冲，缓冲也用不上就挂起自己**。

缓冲容量同样要按这个模型来定：它决定「生产快于消费」时愿意垫付多少内存与延迟，而不是越大越好。若长期看到 `sendq` 堆积（`go tool pprof` 的 goroutine profile 里全是 `chansend`），说明背压已经在了，该加消费者或降低生产速率，而不是继续调大容量。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l4-concurrency/lesson-043-hchan`](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-043-hchan)。clone 仓库后进入 `examples/l4-concurrency`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="用取模实现环形下标回绕" task="写一个小程序：设环形缓冲容量为 3，用取模让 sendx 连续回绕打印 5 次写入位置，再让 recvx 回绕打印 3 次读取位置。观察下标是否按 0、1、2、0、1 的规律前进。" :hints='["下一个位置写成 (sendx + 1) % n","sendx 与 recvx 各自独立回绕","循环条件用不等于比较，避免多余符号"]' solution='package main\n\nimport "fmt"\n\nfunc main() {\n    const n = 3\n    sendx, recvx := 0, 0\n    for i := 0; i != 5; i++ {\n        fmt.Println("send at", sendx)\n        sendx = (sendx + 1) % n\n    }\n    for i := 0; i != 3; i++ {\n        fmt.Println("recv at", recvx)\n        recvx = (recvx + 1) % n\n    }\n}' explain="输出依次是 send at 0、1、2、0、1 与 recv at 0、1、2。取模让下标到末尾自动回到 0，这正是 hchan 的 sendx 与 recvx 能原地复用槽位、不必搬移整个数组的原因。" />

## 随堂检测

<Quiz no="043-1" q="无缓冲 channel 上，发送方到达时已有一个接收者等在 recvq 里，运行时会怎么做？" :options='["把值放进一块临时缓冲区，稍后再交给接收者","直接把值拷贝到该 sudog 指定的接收变量并唤醒它","丢弃这个值并返回，让接收者拿到零值","为这个 channel 新建一个槽位"]' answer="1" explain="有等待的接收者时走直接拷贝路径：值从发送方栈拷到接收方变量，同时 goready 唤醒接收者，完全不经过 buf。这也是无缓冲 channel 能做到同步交接的原因。" />

<Predict no="043-2" q="三个 goroutine 都在同一个无缓冲 channel 上接收，随后依次发送 100、200、300，会看到什么？" :options='["三个接收者全部阻塞，发送方也卡住","三个接收者各被唤醒一次，三条信息都打印","只有第一个接收者被唤醒，其余两个永久泄漏","编译通过但运行时 panic"]' answer="1" output="此时 recvq 里排着 3 个等待的 G\n接收者 2 被唤醒，拿到 300\n接收者 3 被唤醒，拿到 200\n接收者 1 被唤醒，拿到 100\n全部接收者完成" explain="三次发送分别配对 recvq 队首的一个 sudog，逐个唤醒，因此三条信息都会打印且三个值各被取走一次。但哪个编号拿到哪个值、以及打印先后，都由入队与调度顺序决定。" />

## 一页纸速记

<KnowledgeCard :points='["hchan = 环形缓冲 buf + sendq/recvq 两条等待队列 + closed 标志 + 一把 lock","有对面等待者就直接拷贝并唤醒，配对不上才用缓冲，缓冲也用不上就 gopark 挂起自己","sendx / recvx 取模回绕，保证先进先出且不需要搬移数据","select 把同一个 G 的 sudog 挂到所有 case 的队列上，谁先就绪谁唤醒它"]' :pitfalls='["以为 channel 是无锁的，忽略 hchan.lock 在高竞争下的热点","传大结构体按值收发，拷贝与写屏障成本被放大","传指针却在发送后继续改对象，重新引入共享与数据竞争","把缓冲容量当性能开关，无视 sendq 堆积发出的背压信号"]' summary="channel 的全部行为都源于一件事：先配对队列，再退而用环形缓冲，最后才挂起自己。" />

## 小结与下一讲

- `make(chan T, n)` 背后是 hchan：`buf` 环形缓冲、`sendq`/`recvq` 等待队列、`closed` 标志与保护它们的 `lock`。
- 收发优先与对面队列的 `sudog` 直接拷贝并唤醒；配对不上才走缓冲，缓冲不可用就 `gopark` 挂起，这解释了全部阻塞与 panic 规则。
- 大元素按值传递拷贝昂贵，指针虽省拷贝却带来所有权与逃逸问题；`select` 的实现基础就是把一个 G 的 `sudog` 登记到多个队列。

既然一个 G 可以同时等在多条 channel 上，下一讲就把这个能力变成语法：select、超时与多路复用。

[下一讲：select、超时与多路复用](/concurrency/select)
