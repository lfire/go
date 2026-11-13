---
title: 第 038 讲 · 并发与并行：CSP 思想
description: 第 038 讲 · 并发与并行：CSP 思想 —— 并发不等于并行、CSP 与共享内存的取舍、Go 的选择、何时不该并发
---

<StageHeader stage="L4 · 并发编程" level="★★☆☆☆" time="30 分钟" goal="建立正确的并发世界观" prereq="阶段三 · 核心机制" />

# 第 038 讲 · 并发与并行：CSP 思想

> 读本讲前：正文里会出现 `make(chan)`、`chan<-`、`close` 与 `for range ch`，它们的规则在[第 042 讲](/concurrency/channel)才正式展开；这里只需把 channel 理解成「一个能收发数据的通道」。
>
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

### 再深一层：并发不自带同步、失败时序与可验证证据

<Callout type="note" title="进阶选读 · 首次学习可跳过" text="本段涉及运行时源码、内存模型与性能工具等后面讲次才系统展开的内容，属于加深理解的可选层。第一次学建议先掌握本讲主线，遇到尚未讲过的术语不必停下，学完对应讲次再回看，不影响后续理解。" />

「并发能重叠等待」讲的是吞吐，不是正确性。把串行代码拆成多个 goroutine 后，最危险的直觉是以为「它们总有个先后」或「sleep 一下就等到了」。Go 内存模型（本机 `$GOROOT/doc/go_mem.html`）并不为「并发」本身提供任何保证，它只给一条核心承诺：**无数据竞争的程序表现得像所有 goroutine 被复用到单处理器上**，即 DRF-SC；一旦存在竞争，编译器可重排、CPU 可乱序、可见性无约束。由此能推出 CSP 与共享内存两条路线真正的分界——**正确性来自同步动作，不来自并发结构**。channel 的收发与 `close` 是内存模型明确列出的同步动作；Mutex 的 Unlock 与后续 Lock、WaitGroup 的 Done 与 Wait 同样是；而 `time.Sleep`、`runtime.Gosched`、共享 bool 标志都不是。CSP 的好处不是「天生更快」，而是把数据交接点与同步点重合，让漏掉同步更难。

| 动作 | 是否建立同步顺序（即 happens-before，正式定义见第 049 讲） | 依据与边界 |
| --- | --- | --- |
| `go f(args)` | 是，但只到新 G 的起点 | go_mem.html：`go` 语句 synchronized before 新 goroutine 开始执行；不等待其结束 |
| channel 的一次收发、`close(ch)` | 是 | go_mem.html 列明的同步动作，交接点即同步点 |
| Mutex 的 Unlock 到后续 Lock | 是 | `sync` 包注释：第 n 次 Unlock synchronizes before 第 m 次 Lock（n 小于 m） |
| WaitGroup 的 Done 到被唤醒的 Wait | 是 | `sync/waitgroup.go` 注释：Done synchronizes before 它所解除阻塞的 Wait 的返回 |
| `time.Sleep`、`runtime.Gosched`、共享 bool 标志 | 否 | 都不是同步动作；用它「等结果」既不确定，又构成数据竞争 |

先做一个可复现的对照实验，把上面的判据变成能跑的结论。`serialSum` 串行累加无共享写入、天然安全；`sharedSum` 把同一变量交给多个 goroutine 无同步累加，`go test -race` 会精确打印 `WARNING: DATA RACE` 及两段冲突栈。注意反面实验用 `SHOW_RACE=1` 环境变量 gate，保证默认 `go test -race ./...` 全绿、只在需要时暴露问题——这也是把「错误写法」留在代码里做教学对照、又不污染 CI 的惯例（第 45、49 讲沿用同一手法）。

```go
package main

import (
	"os"
	"sync"
	"testing"
	"time"
)

// serialSum 串行累加，无共享写入，安全
func serialSum(n int) int {
	sum := 0
	for i := 0; i < n; i++ {
		sum++
	}
	return sum
}

// sharedSum 并发累加同一变量且无同步，构成数据竞争
func sharedSum(n int) int {
	sum := 0
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sum++
		}()
	}
	wg.Wait()
	return sum
}

func TestSerialSafe(t *testing.T) {
	if got := serialSum(1000); got != 1000 {
		t.Fatalf("串行期望 1000，实际 %d", got)
	}
}

// 反面实验：设 SHOW_RACE=1 并配合 -race，暴露共享累加的 DATA RACE
func TestSharedRace(t *testing.T) {
	if os.Getenv("SHOW_RACE") == "" {
		t.Skip("反面实验：设 SHOW_RACE=1 并加 -race 运行")
	}
	_ = sharedSum(1000)
}

// ioTask 模拟一次 100ms 的 I/O 等待，期间不占 CPU
func ioTask() { time.Sleep(100 * time.Millisecond) }

func BenchmarkSerialIO(b *testing.B) {
	for i := 0; i < b.N; i++ {
		for j := 0; j < 3; j++ {
			ioTask()
		}
	}
}

func BenchmarkConcurrentIO(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for j := 0; j < 3; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ioTask()
			}()
		}
		wg.Wait()
	}
}
```

```bash
go test -race -run=TestSerialSafe -v ./...                       # 正确写法：全绿
SHOW_RACE=1 go test -race -run=TestSharedRace ./...              # 反面实验：报 WARNING: DATA RACE
go test -bench='SerialIO|ConcurrentIO' -benchmem -run=^$ ./...   # 看 ns/op 与 allocs/op
```

<OutputBox title="本机实测（Go 1.23.8 / darwin-arm64）" output="BenchmarkSerialIO-12：约 305 ms/op，0 B/op，0 allocs/op\nBenchmarkConcurrentIO-12：约 101 ms/op，496 B/op，6 allocs/op" note="三段 100ms 等待重叠成约 100ms；并发版多出 3 个 G 的栈与 WaitGroup，故 B/op 与 allocs/op 上升" />

逐字段读这份输出：换算成 `ms/op` 的 `ns/op` 是每轮耗时，串行约等于三段等待之和、并发约等于一段，直接量化「重叠等待」的收益；`B/op` 是每轮堆分配字节数，并发版多出的是 goroutine 起始栈与同步结构的分摊；`allocs/op` 是每轮分配次数，串行版为 0、并发版为 6（3 个 G 加 WaitGroup 的簿记）。把 `ioTask` 换成纯计算循环、`GOMAXPROCS` 设为 1 再跑一遍，两个 benchmark 会几乎持平甚至并发更慢——这就是「没有等待就没有收益」的可测量证据，也说明「用了 goroutine 就一定更快」是错的。

`-race` 能抓什么、抓不到什么，在这个例子里也要分清：它能抓 `sharedSum` 里「读改写同一变量且无 happens-before」的冲突访问；但**抓不到逻辑竞态**——比如两个访问各自都被锁保护了、只是组合顺序错了（检查后使用 TOCTOU），或者下面这类「重复执行 / 部分成功」的业务级时序错误，因为那里未必有无同步的冲突读写。它更不会告诉你的程序「该不该并发」。

并发把单点的顺序性拆开后，原子性、幂等与补偿必须重新设计，否则最典型的失败是「重复执行」和「部分成功」：同一笔业务被两个 worker 读到同一份旧状态、各自提交一次。下面这张时序图画的就是这条链路。

<SequenceDiagram title="盲目并发导致的重复执行" caption="两个 worker 读到同一旧状态并各自提交，订单被重复处理" :actors='["客户端","派发器","workerA","workerB","订单状态"]' :steps='[{"from":"客户端","to":"派发器","label":"提交订单","note":"未带幂等键","kind":"call"},{"from":"派发器","to":"workerA","label":"并发派发","note":"同一订单","kind":"call"},{"from":"派发器","to":"workerB","label":"并发派发","note":"未去重","kind":"call"},{"from":"workerA","to":"订单状态","label":"读未处理","note":"读到旧值","kind":"call"},{"from":"workerB","to":"订单状态","label":"读未处理","note":"也读到旧值","kind":"call"},{"from":"workerA","to":"订单状态","label":"标记完成","note":"第一次提交","kind":"call"},{"from":"workerB","to":"订单状态","label":"重复扣款","note":"检查后使用失效","kind":"async"},{"from":"订单状态","to":"客户端","label":"两次结果","note":"部分成功与资损","kind":"return"}]' />

把工程里最常踩的两条误解写成错误与正确代码对照：

<Compare title="起 goroutine 之后怎么等" :panels='[{"label":"错误：sleep 兜底","code":"for i := 0; i &lt; 3; i++ {\n    go work(i)\n}\ntime.Sleep(1 * time.Second) // 不是同步动作","note":"sleep 既不保证任务已跑完，也不建立 happens-before；负载一高就提前返回，共享结果读到旧值。"},{"label":"正确：显式同步","code":"var wg sync.WaitGroup\nfor i := 0; i &lt; 3; i++ {\n    wg.Add(1)\n    go func(id int) {\n        defer wg.Done()\n        work(id)\n    }(i)\n}\nwg.Wait()","note":"Done synchronizes before Wait 的返回，任务写入对 main 可见；也可用 channel 收结果。"}]' note="等待要用同步原语，不能用时长去猜。" />

<Callout type="warn" title="第二条误解：以为并发结构自带正确性" text="「我用了 channel、起了 goroutine，所以不会有竞争」不成立。用 channel 传递的是数据所有权，若通道里传的是指针、发送后还继续改对象，或另开一个共享 bool 当停止标志，仍然是无同步的并发访问。判断标准永远是：这两次访问之间有没有一条 happens-before 边，而不是「代码看起来是不是并发风格」。" />

<Compare title="用了 channel 就安全了吗" :panels='[{"label":"错误：通道之外另开共享变量","code":"ch := make(chan int, 1)\nvar total int\ngo func() {\n    ch &lt;- 1\n    total++ // 与 main 的读无同步\n}()\n&lt;-ch\nfmt.Println(total)","note":"channel 只给这一次收发送了同步边；total 的自增与 main 的读之间没有 happens-before，仍是数据竞争。"},{"label":"正确：让数据随通道交接","code":"ch := make(chan int, 1)\ngo func() { ch &lt;- 1 }()\ntotal := &lt;-ch // 值本身就是交接过来的\nfmt.Println(total)","note":"把要共享的值放进通道，交接点即同步点；不再另开一个裸共享变量。"}]' note="CSP 的价值是让同步点与交接点重合，不是免掉同步。" />

最后把这些结论接到后面的实战里。第 109 讲 [并发爬虫](/projects/concurrent-crawler) 的 V1 单线程把 CPU 空转在 RTT 等待上，V2 用 worker pool 重叠等待才把吞吐拉起来，正是本讲「先找到等待再并发」的完整落地；但 V2/V3 必须补上 URL 去重与幂等键，否则并发抓取会重复下载、结果表重复写入——这恰好是上面「重复执行」时序图的真实版本。第 110 讲 [分布式任务调度](/projects/concurrent-scheduler) 把「定时任务执行两次」列为资损事故，用选主、状态机与 fencing token 收敛重复执行；第 116 讲 [秒杀与高并发库存](/projects/micro-seckill) 里「先查余量再扣减」在并发下会超卖，必须把检查与扣减合成一个原子步骤。三处都在回答同一个问题：**并发提高了吞吐，正确性得靠同步与幂等重新挣回来**。

<Quiz q="把三个各等待 100ms 的 I/O 任务改成 goroutine 并发，但共享一个结果变量且没有任何同步，go test -race 最可能看到什么？" :options='["全绿，因为并发本来就是安全的","WARNING: DATA RACE，指向该变量的读写","编译报错，禁止在 goroutine 里写共享变量","只是结果偶尔偏小，无任何提示"]' answer="1" explain="并发结构不建立 happens-before；共享变量的读改写无同步即数据竞争，-race 的向量时钟会报两段冲突栈。要重叠等待又要正确，须用 channel 交接或 WaitGroup 加同步，而不是靠 sleep。" />

## 4. 工程实践

真实服务里最常见的收益来自 I/O：一个 HTTP handler 往往要查数据库、调下游接口、写日志，这些等待彼此独立，用 goroutine 重叠起来就能显著降低尾延迟。而「保护一个共享计数」「缓存一张配置表」这类需求，本质是互斥而不是传递数据，用 `sync.Mutex` 或原子操作更直接。判断口诀是：要交付数据所有权、要编排阶段、要广播信号，选 channel；只想保护一小段临界区，选锁。

<Compare title="两条路线的对照" :panels='[{"label":"共享内存 + 锁","code":"var mu sync.Mutex\nvar count int\nmu.Lock()\ncount++\nmu.Unlock()","note":"多个执行流直接读写同一变量，靠锁圈出临界区；要自己保证每次访问都记得加锁。"},{"label":"CSP 通道","code":"ch := make(chan int, 1)\n// 数据沿通道交接\n// 所有权随消息转移","note":"数据沿通道流动，交接点即同步点；但为了一个计数器绕一圈通道反而更绕。"}]' note="没有绝对正确的选择，只有与问题匹配的选择：传递数据用通道，保护状态用锁。" />

还有一类明确的「不该并发」：步骤之间有严格先后依赖（必须先解析配置再启动服务）、单次工作量极小（微秒级计算）、以及 CPU 已经跑满且没有等待的纯计算流水线。这几种情况下，并发带来的上下文切换与内存占用是净亏损。工程上更稳妥的做法是先写正确的串行版本，用压测或 `pprof` 找到真正卡在等待上的瓶颈，再局部引入并发。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l4-concurrency/lesson-038-concurrency-intro`](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-038-concurrency-intro)。clone 仓库后进入 `examples/l4-concurrency`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="把串行执行改成并发执行" task="有三个编号 1、2、3 的工作函数，请改成用 goroutine 并发执行，并保证 main 在三者全部结束后才退出。要求用 sync.WaitGroup，且不要把循环变量直接捕获进闭包。" :hints='["wg.Add(1) 必须写在 go 语句之前","把编号作为函数参数传进闭包，而不是在闭包里直接引用循环变量","main 的最后一行调用 wg.Wait()"]' solution='package main\n\nimport (\n    "fmt"\n    "sync"\n)\n\nfunc work(id int) {\n    fmt.Println("任务", id, "完成")\n}\n\nfunc main() {\n    var wg sync.WaitGroup\n    for i := 1; i &lt;= 3; i++ {\n        wg.Add(1)\n        go func(id int) {\n            defer wg.Done()\n            work(id)\n        }(i)\n    }\n    wg.Wait()\n    fmt.Println("全部结束")\n}' explain="Add 在 go 之前计数，Done 用 defer 保证异常路径也能减一，Wait 让 main 等所有 goroutine 收尾。把 i 作为参数传入闭包，可避开老版本 Go 的循环变量捕获陷阱；若删掉 Wait，main 会直接退出，三个任务很可能一行都来不及打印。" />

## 随堂检测

<Quiz no="038-1" q="关于并发与并行，下面哪一种说法是准确的？" :options='["并发必须有多核 CPU 才能实现","并行是并发的子集，先有并行才有并发","并发是关于结构与应对多件事，并行是关于同一时刻真的在做多件事","只要用了 goroutine，程序就一定在并行执行"]' answer="2" explain="并发描述程序被组织成多个可独立推进的活动，单核上通过交替执行也能做到；并行是这些活动在同一时刻真的同时运行，需要多个执行单元。二者是不同维度，不存在包含关系。" />

<Predict no="038-2" q="把三个各等待 100ms 的 I/O 任务从串行改成三个 goroutine 并发执行，总耗时最接近哪个值？" :options='["仍约 300ms","约 100ms","约 33ms","一定比串行更慢"]' answer="1" output="串行耗时: 300ms\n并发耗时: 100ms" explain="等待时间可以重叠，三段 100ms 的等待合并成约 100ms。缩短的是等待而不是计算；若三个任务都是占满 CPU 的纯计算且只有一个 P，并发就不会带来这个收益。" />

## 一页纸速记

<KnowledgeCard :points='["并发是结构（同时应对多件事），并行是执行状态（同一时刻真的在做多件事），单核也能并发","CSP 主张顺序进程不共享内存，只通过通道传递消息与所有权；共享内存路线靠锁圈临界区","Go 的默认答案是 goroutine 加 channel，但保留 sync 与 atomic 用于纯互斥场景","并发的收益来自重叠等待，先找到等待再并发"]' :pitfalls='["误以为用了 goroutine 就一定更快，忽略纯计算与协调开销","把「保护一个计数器」硬绕成通道，增加无谓复杂度","步骤有严格先后依赖、任务极小、CPU 已跑满时仍然盲目并发"]' summary="并发是关于怎么组织，CSP 是关于怎么交接：先分清等待与计算，再选通道或锁。" />

## 小结与下一讲

- 并发与并行是两个维度：前者是应对多件事的结构，后者是同时运行的状态，单核照样能并发。
- CSP 用通道传递数据所有权，交接点天然是同步点；共享内存加锁则适用于纯互斥的小临界区，Go 两者都保留。
- 并发的收益来自重叠等待；没有等待、有严格依赖或任务过小时，串行版本往往是更好的工程选择。

下一讲我们落到具体的执行单元上：goroutine 到底轻在哪里、栈怎么长、什么时候会泄漏。

[下一讲：goroutine：创建、栈与生命周期](/concurrency/goroutine)
