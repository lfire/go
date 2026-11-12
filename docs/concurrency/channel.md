---
title: 第 042 讲 · channel：语义、缓冲与关闭
description: 第 042 讲 · channel：语义、缓冲与关闭 —— 无缓冲与缓冲、发送接收阻塞规则、关闭与遍历、只发送只接收、nil channel
---

<StageHeader stage="L4 · 并发编程" level="★★★☆☆" time="40 分钟" goal="用 channel 正确传递数据与信号" prereq="第 041 讲 · 调度循环：抢占、窃取与 handoff" />

# 第 042 讲 · channel：语义、缓冲与关闭

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

### 再深一层：channel 的 happens-before 清单、关闭时序与可验证证据

channel 为什么「既传数据又同步」，不是一句口诀，而是 `go_mem.html` 里四条明确的规则。逐条看，才能把上一讲那些阻塞与 panic 行为一路推到底：

- **发送 synchronized before 对应接收的完成**：`A send on a channel is synchronized before the completion of the corresponding receive from that channel.`。发送前的写入，对接收完成之后的读可见——这就是用通道「发布」一份数据的依据。
- **无缓冲的接收 synchronized before 发送完成**：`A receive from an unbuffered channel is synchronized before the completion of the send on that channel.`。所以无缓冲的「发送返回」才敢说「对方已经接走」。
- **关闭 synchronized before 读到零值的那次接收**：`The closing of a channel is synchronized before a receive that returns a zero value because the channel is closed.`。`close(done)` 之所以能替代共享 bool 广播叫停大批 goroutine，靠的正是这条（第 44、47 讲会再用）。
- **缓冲槽位复用也带同步**：`The kth receive on a channel with capacity C is synchronized before the completion of the (k+C)th send from that channel completes.`。即使不走直接拷贝、只是复用同一个环形槽位，同步边依然成立——这条最常被漏掉。

机器级依据在 `runtime/chan.go`：`chansend`、`chanrecv`、`closechan` 都在 `hchan.lock` 的保护下把阻塞方包成 `sudog` 挂进等待队列，配对时直接拷贝值并 `goready` 唤醒（结构见第 043 讲）。锁的 acquire/release 加上 goroutine 唤醒，共同实现了上面这四条同步动作。反过来，共享 bool 标志、`time.Sleep` 轮询、`len(ch)` 快照都不在清单里，用它们「判断完成」或「判断能不能发」都没有正确性依据。

| 动作 | 保证 | 不保证 / 边界 |
| --- | --- | --- |
| 缓冲 channel 发送返回 | 值已入队；第 k 次接收与第 k+C 次发送之间有同步 | 不保证已被消费方处理 |
| 无缓冲发送返回 | 接收方已把值接走 | 不保证接收方后续处理完成 |
| `close(ch)` | 关闭前写入对所有接收者可见 | 不保证接收者已处理完缓冲里的剩余数据 |
| `len(ch)` / `cap(ch)` | 某一瞬间的快照值 | 不作同步，不能据此判断「可安全发送」 |
| 共享 bool 标志、`time.Sleep` 轮询 | 什么都不保证 | 是数据竞争，且时长不确定 |

`-race` 能抓的是「共享 bool 停止标志」这种无同步的冲突读写；**抓不到**「用 `len(ch)` 判断后再发送」这类检查后使用（TOCTOU）的逻辑竞态——那里两次访问不一定构成冲突读写；也**抓不到**「多个生产者抢着关闭」导致的 `send on closed channel` panic，那是运行时错误而非数据竞争。下面把两类写法做成可复现对照：`publishOK` 用通道发布全绿，`publishBad` 用普通 bool 发布，`-race` 打印两段冲突栈。反面实验同样用 `SHOW_RACE=1` gate，保证默认回归全绿。

```go
package main

import (
	"os"
	"sync"
	"testing"
	"time"
)

// publishBad 用普通 bool 发布，写方与读方无 happens-before
func publishBad() int {
	var data int
	var ready bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		data = 42
		ready = true
	}()
	for i := 0; i < 1000 && !ready; i++ {
	}
	wg.Wait()
	return data
}

// publishOK 用 channel 发布，收发生成 happens-before
func publishOK() int {
	ch := make(chan int, 1)
	go func() {
		ch <- 42
	}()
	return <-ch
}

func TestPublish(t *testing.T) {
	for i := 0; i < 100; i++ {
		if got := publishOK(); got != 42 {
			t.Fatalf("期望 42，实际 %d", got)
		}
	}
}

// 反面实验：设 SHOW_RACE=1 并配合 -race，暴露 bool 发布的 DATA RACE
func TestPublishBadRace(t *testing.T) {
	if os.Getenv("SHOW_RACE") == "" {
		t.Skip("反面实验：设 SHOW_RACE=1 并加 -race 运行")
	}
	_ = publishBad()
}

// stopBad 用共享 bool 当停止标志，是数据竞争
func stopBad(stop *bool) {
	for !*stop {
		time.Sleep(time.Millisecond)
	}
}

// stopOK 用 close(done) 广播，关闭与接收建立 happens-before
func stopOK(done <-chan struct{}) {
	<-done
}

func TestStop(t *testing.T) {
	done := make(chan struct{})
	go stopOK(done)
	close(done)
}

func BenchmarkUnbuffered(b *testing.B) {
	ch := make(chan int)
	go func() {
		for i := 0; i < b.N; i++ {
			ch <- 1
		}
	}()
	for i := 0; i < b.N; i++ {
		<-ch
	}
}

func BenchmarkBuffered(b *testing.B) {
	ch := make(chan int, 64)
	for i := 0; i < b.N; i++ {
		ch <- 1
		<-ch
	}
}
```

```bash
go test -race -run='TestPublish|TestStop' -v ./...             # channel 发布与 close 停止：全绿
SHOW_RACE=1 go test -race -run=TestPublishBadRace ./...        # bool 发布：报 DATA RACE 两段栈
go test -bench='Unbuffered|Buffered' -benchmem -run=^$ ./...   # 看 ns/op 与 B/op
go test -run=^$ -bench=BenchmarkUnbuffered -blockprofile=block.out ./... && go tool pprof -top -block block.out
```

<OutputBox title="反面实验的 -race 报告（示意）" output="WARNING: DATA RACE\nWrite at 0x00c0000140a0 by goroutine 8:\n  main.publishBad.func1()\n      publish_test.go:16 +0x30\nPrevious read at 0x00c0000140a0 by main goroutine:\n  main.publishBad()\n      publish_test.go:19 +0x54" note="两段栈分别指向 ready 的写与读；改回 channel 发布后报告消失" />

逐字段读：`Write at` 后面是冲突地址与写方 goroutine 编号，`Previous read at` 是同一地址的读方，各自下面的函数名、文件行号与 `+0x30` 这类 PC 偏移定位到具体语句。读法很简单——顺着两段栈找哪两次访问之间缺同步；这里正是 `ready = true` 与自旋读 `!ready`。改成 `publishOK` 的通道收发后，一次发送 synchronized before 接收完成，同步边补上，报告消失。

<OutputBox title="BenchmarkUnbuffered 与 BenchmarkBuffered 本机实测（Go 1.23.8 / darwin-arm64）" output="BenchmarkUnbuffered-12：约 280 ns/op，0 B/op，0 allocs/op\nBenchmarkBuffered-12：约 30 ns/op，0 B/op，0 allocs/op" note="无缓冲每次收发都要双方配对，缓冲 64 让发送直接入队" />

`ns/op` 的差距来自交接方式：无缓冲每次都要把发送方或接收方挂进等待队列再 `goready` 唤醒，约 280ns；缓冲 64 时发送直接写入环形槽位、接收直接取出，约 30ns。`B/op` 与 `allocs/op` 都为 0，是因为 hchan 与缓冲区在 `make` 时一次性分配，稳态收发不产生新的堆分配。`go tool pprof -top -block` 则能看见无缓冲版本里卡在 `chansend` / `chanrecv` 的累计阻塞时长，把「同步交接更贵」从数字变成画像。

失败时序集中在收尾：多个生产者时任何一家提前 `close`，其它家就会撞上 `send on closed channel` 的 panic，而已经发出去的结果只被部分消费，形成「部分成功」。下面这张时序图把这条链路与正确做法并排画出来。

<SequenceDiagram title="多生产者抢着关闭的失败链路" caption="任一生产者提前 close 会让其它生产者 panic，消费者只拿到部分结果" :actors='["生产者A","生产者B","channel","消费者","协调者"]' :steps='[{"from":"生产者A","to":"channel","label":"发送数据","note":"第一个写入","kind":"call"},{"from":"生产者B","to":"channel","label":"发送数据","note":"第二个写入","kind":"call"},{"from":"生产者A","to":"channel","label":"提前 close","note":"违反谁发送谁关闭","kind":"call"},{"from":"channel","to":"生产者B","label":"send 已关闭","note":"panic 风险","kind":"async"},{"from":"生产者B","to":"channel","label":"触发 panic","note":"send on closed","kind":"call"},{"from":"消费者","to":"channel","label":"range 收尾","note":"只读剩余数据","kind":"call"},{"from":"channel","to":"消费者","label":"剩余数据","note":"结果不完整","kind":"return"},{"from":"协调者","to":"channel","label":"等齐再 close","note":"正确做法","kind":"call"},{"from":"channel","to":"消费者","label":"完整收尾","note":"部分成功被消除","kind":"return"}]' />

两条工程误解都源自「把快照或共享变量当同步」：

<Compare title="别用共享 bool 当停止标志" :panels='[{"label":"错误：共享变量加轮询","code":"var stop bool\ngo worker(&stop)\ntime.Sleep(50 * time.Millisecond)\nstop = true // 与 worker 的读无同步","note":"写与读之间没有 happens-before，是数据竞争；sleep 时长也不确定，go test -race 会报。"},{"label":"正确：close 广播","code":"done := make(chan struct{})\ngo worker(done)\nclose(done) // 关闭与所有接收建立 HB","note":"关闭 channel 是内存模型列明的同步动作，一次广播唤醒全部等待者。"}]' note="「停止」这类信号尤其要用同步原语，不能用时间或共享变量去猜。" />

<Compare title="别拿 len 当发送许可" :panels='[{"label":"错误：检查后使用","code":"if len(ch) &lt; cap(ch) {\n    ch &lt;- v // 之间可能被别人填满\n}","note":"len 是快照、不是同步动作；判断与发送之间缓冲区可能已被别的生产者占满，属逻辑竞态，-race 抓不到。"},{"label":"正确：非阻塞探测或靠背压","code":"select {\ncase ch &lt;- v:\ndefault:\n    // 满了就降级或等背压\n}","note":"select 的 case 就绪是运行时判定的，或直接发送让缓冲满自然阻塞形成背压。"}]' note="能不能发，由 channel 的阻塞规则说了算，不由 len 的瞬时值说了算。" />

<Callout type="note" title="把规则串起来" text="第 043 讲会拆开 hchan，解释这四条同步动作在运行时怎么落地；第 044 讲用 close(done) 与 select 搭超时停止；第 047 讲用 context 树把取消沿调用链传播；第 048 讲把「谁写谁关」当成 pipeline 收尾的第一纪律。本讲的四条红线与四条 happens-before 规则，是后面所有套路的公共地基。" />

这些规则在实战里几乎逐条出现。第 109 讲 [并发爬虫](/projects/concurrent-crawler) 的 Frontier 是有界队列，生产端与 worker 之间靠「谁发送谁关闭」收尾，队列满时背压到生产端而不是静默丢任务；第 107 讲 [日志分析 ETL](/projects/data-log-etl) 用 pipeline 串起读取、解析、聚合、落库，阶段间 channel 的 close 顺序直接决定会不会漏数据；第 110 讲 [分布式任务调度](/projects/concurrent-scheduler) 用 close 广播触发优雅退位、用超时释放租约；第 116 讲 [秒杀与高并发库存](/projects/micro-seckill) 的有界队列削峰、结果轮询与超时释放，正是「缓冲容量即允许积压量」的工程版本。

<Quiz q="多个 goroutine 共用一个缓冲 channel，用 if len(ch) 小于 cap(ch) 判断后再发送，为什么仍可能出问题？" :options='["len 会触发 panic","判断与发送之间别的生产者可能先把缓冲填满，这是检查后使用的逻辑竞态，-race 抓不到","channel 不支持 len","编译报错"]' answer="1" explain="len 只是某一瞬间的快照，不是同步动作；判断与发送之间缓冲区可能已被别人填满。这类 TOCTOU 逻辑竞态里未必有无同步的冲突读写，-race 不查。正确做法是用 select 加 default 做非阻塞探测，或直接靠背压发送。" />

## 4. 工程实践

关闭协议的第一条纪律：**谁发送，谁关闭**。若只有一个生产者（含用 `go` 启动的那一个），它填完数据后直接 `close` 即可，消费者用 `for range` 收尾，代码最干净。若有多个生产者，任何一家提前关闭都会让其它家撞上「向已关闭 channel 发送」的 panic；这时要引入额外的完成信号，等所有生产者都收工后再由一个协调者统一关闭——具体做法会在 sync 与 context 相关讲次展开，本讲先记住「不要在消费者里关闭」这条底线。

<Compare title="无缓冲与缓冲怎么选" :panels='[{"label":"无缓冲","code":"ch := make(chan int)\n// 强同步，收发必须同时就绪","note":"发送返回即代表对方已接走，适合信号、请求应答与严格的阶段同步；代价是双方节奏完全耦合。"},{"label":"缓冲","code":"ch := make(chan int, 64)\n// 允许突发，削峰填谷","note":"生产者不必等消费者，能吸收短时峰值；但发送成功不等于处理完成，容量拍太大就失去背压。"}]' note="容量不是越大越好：它决定你愿意为「生产快于消费」垫付多少内存与延迟。" />

还要克制一个误解：channel 不是消息队列。它没有持久化、没有重试、没有确认机制，进程一退出数据就没了；它只是**同一进程内** goroutine 之间的同步交接原语。跨服务、要可靠投递的场景该用真正的 MQ。另外别为了「传个信号」去造带数据的 channel，`chan struct{}` 更省内存、意图也更清楚。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l4-concurrency/lesson-042-channel`](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-042-channel)。clone 仓库后进入 `examples/l4-concurrency`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="验证关闭后 range 能干净收尾" task="创建一个容量为 2 的缓冲 channel，不做任何发送就立刻关闭，然后用 for range 遍历它，并在循环后打印一行提示。观察循环是正常退出还是永久阻塞，并解释原因。" :hints='["close 可以在没有任何发送的情况下调用","range 在 channel 关闭且缓冲排空时会自然结束","循环之后打印一行文字，就能证明程序没有卡住"]' solution='package main\n\nimport "fmt"\n\nfunc main() {\n    ch := make(chan int, 2)\n    close(ch)\n    for v := range ch {\n        fmt.Println(v)\n    }\n    fmt.Println("遍历结束")\n}' explain="range 的终止条件是「channel 已关闭且缓冲已排空」，与是否发送过数据无关。因此循环体一次都不执行，程序直接打印「遍历结束」，不会阻塞也不会 panic。" />

## 随堂检测

<Quiz no="042-1" q="向一个已经关闭的 channel 发送数据，会发生什么？" :options='["立即返回元素零值","触发 panic","永久阻塞","数据被静默丢弃"]' answer="1" explain="关闭意味着「不会再有数据了」，此时再发送属于违反协议，运行时直接 panic。接收方向则相反：关闭后仍可接收剩余数据，排空后无限次返回零值与 ok=false。" />

<Predict no="042-2" q="容量为 2 的缓冲 channel 里放入 10、20 后关闭，再用 for range 遍历，最后再接收一次，会看到什么？" :options='["遍历输出 10 20，最后一次拿到 0 且 ok=false","遍历输出 10 20，最后一次永久阻塞","遍历直接 panic，因为 channel 已关闭","遍历只输出 10，20 随关闭被丢弃"]' answer="0" output="取出: 10\n取出: 20\n再次接收: v=0 ok=false" explain="关闭不会丢弃缓冲里已排队的数据，range 会先把它们取空再退出。此后继续接收会立刻返回零值与 ok=false，既不阻塞也不 panic。" />

## 一页纸速记

<KnowledgeCard :points='["无缓冲是同步交接，发送返回即代表接收完成；缓冲是异步暂存，容量即允许积压量","关闭由唯一发送方负责，range 在「已关闭且排空」时自然退出","已关闭 channel 可继续接收：剩余数据照常取，之后返回零值加 ok=false","只发送与只接收类型把意图写进签名，由编译器检查"]' :pitfalls='["向已关闭 channel 发送、重复关闭、关闭 nil channel 都会 panic","忘记 make 得到 nil channel，收发永久阻塞且毫无报错","在消费者里关闭 channel，或让多个生产者各自关闭","把 channel 当持久化消息队列，或把缓冲容量拍得过大而丢掉背压"]' summary="channel 的规则可以浓缩成一句：发送方负责关闭，接收方负责收尾，关闭之后只读不写。" />

## 小结与下一讲

- 无缓冲 channel 是同步交接，缓冲 channel 允许错峰；`len` 是排队元素数，容量决定背压强度。
- 关闭由唯一发送方完成，`for range` 是最稳的消费方式；关闭后数据仍可读，排空后返回零值与 `ok=false`。
- 三种 panic、一种永久阻塞是高频错误源；单向类型与「谁发送谁关闭」的约定能防住大部分。

规则记牢了，下一讲我们把 channel 拆开，看运行时到底用什么结构把「阻塞的收发双方」配对起来。

[下一讲：channel 底层 hchan 与收发队列](/concurrency/hchan)
