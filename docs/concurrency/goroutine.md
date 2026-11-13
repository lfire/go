---
title: 第 039 讲 · goroutine：创建、栈与生命周期
description: 第 039 讲 · goroutine：创建、栈与生命周期 —— go 关键字、初始栈与增长、调度单位、泄漏与排查、GOMAXPROCS
---

<StageHeader stage="L4 · 并发编程" level="★★☆☆☆" time="35 分钟" goal="知道协程轻在哪里、会怎么泄漏" prereq="第 038 讲 · 并发与并行：CSP 思想" />

# 第 039 讲 · goroutine：创建、栈与生命周期

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

### 再深一层：go 语句的 happens-before、泄漏时序与可验证证据

<Callout type="note" title="进阶选读 · 首次学习可跳过" text="本段涉及运行时源码、内存模型与性能工具等后面讲次才系统展开的内容，属于加深理解的可选层。第一次学建议先掌握本讲主线，遇到尚未讲过的术语不必停下，学完对应讲次再回看，不影响后续理解。" />

「main 不会等 goroutine」只是现象，背后的规则写在 Go 内存模型里。`go_mem.html` 对 `go` 语句的唯一承诺是：**`go` 语句 synchronized before 新 goroutine 开始执行**。由此能推出一条完整的可见性边界：`go` 之前 main 的所有写入，对新 G 的起点可见；反过来，新 G 里的写入对 main 之后的读**不自动可见**——两者此后没有任何排序，除非再引入 channel、`sync.Mutex`、`WaitGroup` 或原子操作。所以「起了 goroutine 看不到输出」与「共享变量读到旧值」是同一件事的两面：都缺一条同步边。`sync/waitgroup.go` 的注释补上另一半：`Done` synchronizes before 它所解除阻塞的 `Wait` 的返回，因此 `wg.Wait()` 之后读任务写入是安全的，前提仍是 `Add` 在 `go` 之前（否则计数可能还没登记，见第 045 讲）。

| 写法 | 保证 | 不保证 / 边界 |
| --- | --- | --- |
| `go f(args)` | 参数在 `go` 处求值；`go` 前写入对 f 起点可见 | 不等待 f 完成、不保证 f 何时开始、不覆盖 f 结束后的可见性 |
| 闭包直接捕获外层变量 | 语法允许 | 与 main 后续访问无排序，属数据竞争 |
| main 里 `time.Sleep` 等待 | 什么都不保证 | 不是同步动作，时长不确定，高负载下失效 |
| `wg.Wait()` | Done 前写入对 Wait 返回后可见 | `Add` 落在 goroutine 内时 Wait 可能提前返回 |

泄漏是这套规则最典型的失败形态：一条阻塞路径永远等不到唤醒，G 就一直挂在等待队列里，占着栈与调度簿记。关键在于它是**缓慢失血**——单次 `runtime.NumGoroutine()` 看不出问题，随请求数线性累积后内存与 GC 才逐步恶化。下面这张时序图把「每个请求漏一个 G」画成一条链路。

<SequenceDiagram title="goroutine 泄漏的累积链路" caption="后台 G 挂在无人唤醒的 channel 上，随请求数单调增长" :actors='["请求","Handler","后台G","结果通道","监控"]' :steps='[{"from":"请求","to":"Handler","label":"到达","note":"每请求一次","kind":"call"},{"from":"Handler","to":"后台G","label":"go 启动","note":"HB 只到起点","kind":"call"},{"from":"Handler","to":"请求","label":"立即返回","note":"main 不等 G","kind":"return"},{"from":"后台G","to":"结果通道","label":"等待结果","note":"无人发送","kind":"call"},{"from":"结果通道","to":"后台G","label":"永久阻塞","note":"gopark 无唤醒","kind":"async"},{"from":"请求","to":"Handler","label":"更多请求","note":"每次再漏一个","kind":"call"},{"from":"后台G","to":"监控","label":"计数上涨","note":"NumGoroutine 单调增","kind":"async"},{"from":"监控","to":"Handler","label":"触发告警","note":"数天后 OOM","kind":"return"}]' />

`-race` 在这里帮不上忙：泄漏的 G 只是 `gopark` 在 `chanrecv` 上，不涉及任何冲突读写，`go test -race` 会全程全绿。这是「-race 全绿不等于没有并发 bug」最直接的证据。定位泄漏要用另外三类手段：`runtime.NumGoroutine()` 的**前后趋势**、`go.uber.org/goleak` 在测试收尾的 `VerifyTestMain`，以及 `go tool pprof` 的 goroutine profile 里那些卡在 `chanrecv` 与 `semacquire` 的 G。

```go
package main

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

// leaking 启动一个永远等不到数据的 goroutine，制造泄漏
func leaking() <-chan struct{} {
	blocked := make(chan struct{})
	go func() {
		<-blocked // 无人 close，永久 gopark
	}()
	return blocked
}

// correct 用 close 广播给阻塞的 G 一个明确的退出路径
func correct() {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-done
	}()
	close(done) // 关闭即广播，与接收建立 happens-before
	wg.Wait()
}

func TestExitPath(t *testing.T) {
	before := runtime.NumGoroutine()
	correct()
	time.Sleep(10 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("存在未回收的 G：前 %d 后 %d", before, after)
	}
}

func BenchmarkSpawnG(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done() }()
		wg.Wait()
	}
}
```

```bash
go test -race -run=TestExitPath -v ./...                     # 正确退出路径：前后 G 数持平，全绿
GODEBUG=schedtrace=1000,scheddetail=1 go run main.go         # 看 gomaxprocs/threads/runqueue 与各 P 的 g
go test -bench=BenchmarkSpawnG -benchmem -run=^$ ./...       # 看一次 go 加 Wait 的 ns/op 与 allocs/op
# go.uber.org/goleak 的 TestMain 里调用 goleak.VerifyTestMain(m)，收尾会报 found unexpected goroutines
```

<OutputBox title="GODEBUG=schedtrace=1000,scheddetail=1 的一段采样" output="SCHED 1000ms: gomaxprocs=12 idleprocs=10 threads=15 spinningthreads=0 idlethreads=6 runqueue=0\n[P 0] g=1 m=0 schedtick=312 syscalltick=18 runqsize=1" note="每 1000ms 打印一行；scheddetail=1 时再补每个 P 一行" />

逐字段解释这行输出：`gomaxprocs` 是 P 数，`idleprocs` 是空闲 P 数，`threads` 是运行时持有的 M（OS 线程）总数，`spinningthreads` 是正在「自旋找活」的 M 数，`idlethreads` 是空闲 M 数，`runqueue` 是全局队列长度。`scheddetail=1` 追加的 `[P 0]` 行里，`g` 是该 P 本地队列里等待的 G 数、`schedtick` 是调度次数、`syscalltick` 是系统调用次数、`runqsize` 是本地队列长度。若持续看到 `threads` 上涨、`idleprocs` 不少而请求延迟仍高，瓶颈多半在阻塞式调用或锁，而不是 G 不够——这时该查的是等待队列，不是起更多 goroutine。

<OutputBox title="go.uber.org/goleak 的典型报告" output="goleak: found unexpected goroutines:\n[Goroutine 19 in state chan receive, with main.leaking.func1 on top of the stack:\n goroutine 19 [chan receive]:\n main.leaking.func1()\n     /path/leak_test.go:12 +0x28]" note="引入 goleak 后，TestMain 收尾会打印这段；-race 对同一份泄漏代码则全绿" />

逐字段读 goleak 报告：编号 19 是残留 G 的 ID，`state chan receive` 说明它挂在 channel 接收上，`main.leaking.func1` 是栈顶函数，文件行号 `leak_test.go:12` 精确指向那条没有发送者的接收语句，`+0x28` 是程序计数器偏移。它把「哪个函数、哪一行、以什么状态」一次给全，比单点 `NumGoroutine()` 有用得多。

<OutputBox title="BenchmarkSpawnG 本机实测（Go 1.23.8 / darwin-arm64）" output="BenchmarkSpawnG-12：约 2000 ns/op，0 B/op，0 allocs/op" note="一次 go 加 Wait 约 2 微秒；稳定后每轮几乎不计新的堆分配" />

`ns/op` 约 2 微秒是一次「创建 G、调度、回收、Wait 返回」的端到端成本，这就是「轻」的量化证据；`B/op` 与 `allocs/op` 稳定为 0 并非「G 没有栈」，而是运行时从栈缓存按批分配约 2 KB 的起始栈并在 G 终止后复用，摊到每轮几乎不再产生新的堆分配。换成同等数量的 OS 线程，光创建与固定栈的内存就是两个数量级的差别。

两条工程误解值得单独对照。

<Compare title="怎么确认后台任务真的收尾了" :panels='[{"label":"错误：用时长或单点值兜底","code":"go background()\ntime.Sleep(1 * time.Second)\n_ = runtime.NumGoroutine() // 单点采样","note":"sleep 不是同步动作，单点 NumGoroutine 看不出趋势；负载一变就失效，泄漏照样累积。"},{"label":"正确：退出路径加收尾校验","code":"done := make(chan struct{})\ngo func() { defer wg.Done(); &lt;-done }()\nclose(done) // 广播唤醒\nwg.Wait()\n// 测试收尾 goleak.VerifyNone(t)","note":"每条阻塞路径都有 close 或 ctx.Done() 出口，Wait 建立可见性，goleak 在收尾兜底。"}]' note="防泄漏的纪律是「谁造成阻塞，谁负责给唤醒」，不是靠等。" />

<Callout type="warn" title="第二条误解：NumGoroutine 稳定就等于没泄漏" text="泄漏是缓慢失血，几天的趋势才看得出来；而且运行时辅助 G、测试框架自身都会让单点值上下浮动。要看的是随请求数是否单调增长，配合 goleak 与 goroutine profile 定位阻塞行，而不是拿一个数字当判据。同理，「泄漏会立刻崩溃」也错——它先吞内存与调度时间，最后才 OOM。" />

<Compare title="怎么观测才看得出泄漏" :panels='[{"label":"错误：拿单点值下结论","code":"if runtime.NumGoroutine() &lt; 100 {\n    fmt.Println(\"没有泄漏\") // 看不出趋势\n}","note":"运行时辅助 G、测试框架都会让单点值浮动；100 这个阈值也没有依据，泄漏照样累积。"},{"label":"正确：看增量并收尾校验","code":"before := runtime.NumGoroutine()\nhandleRequests()\nafter := runtime.NumGoroutine()\nfmt.Println(\"增量:\", after-before)\n// 测试收尾 goleak.VerifyNone(t)","note":"随请求数是否单调增长才是信号；goleak 与 goroutine profile 能定位到具体阻塞行。"}]' note="判断泄漏看趋势与阻塞栈，不看一个孤立数字。" />

这些纪律在后面的实战里会反复出现。第 109 讲 [并发爬虫](/projects/concurrent-crawler) 的 V2 worker pool 里，每个 Fetcher worker 必须能从 `jobs` 关闭或 `ctx.Done()`（取消信号，第 047 讲展开）退出，否则优雅停机停不下来、在途 G 越积越多；第 110 讲 [分布式任务调度](/projects/concurrent-scheduler) 把「发布重启时在途任务被硬杀、既没记录也没补偿」列为痛点，正需要可取消的退出路径加状态落盘；第 116 讲 [秒杀与高并发库存](/projects/micro-seckill) 里每个请求一个 G，若下游没有超时或停止出口，洪峰里会瞬间堆积大量挂起的 G。呼应本讲一句话：**起得便宜，更要保证每条阻塞路径都等得到唤醒**。

<Predict q="一个后台 goroutine 永久阻塞在无人发送的 channel 上，用 go test -race 跑会怎样？" :options='["报 WARNING: DATA RACE","测试通过无报告，但 goroutine 数与内存随请求数缓慢上涨","直接 panic","编译报错"]' answer="1" output="-race 全程无报告；runtime.NumGoroutine() 每次请求后 +1，goleak 在收尾报 found unexpected goroutines" explain="泄漏是 G 挂在等待队列里无人唤醒，不涉及冲突读写，-race 查不到；它是缓慢失血，要靠 NumGoroutine 趋势、goleak 与 goroutine profile 定位。给每条阻塞路径留 close 或 ctx.Done() 退出路径才是修复。" />

## 4. 工程实践

标准库 `net/http` 的做法就是本讲最典型的落地：每个连接由独立 goroutine 服务，请求结束 G 即退出，不需要线程池。这种模式成立的前提正是「创建便宜、阻塞便宜、回收自动」。工程上要注意三点：给所有后台 goroutine 留可取消的退出路径；用监控持续观察 `runtime.NumGoroutine()` 的趋势而不是单点值；出现堆积时用 `go tool pprof` 的 goroutine profile 看阻塞在哪一行。

<Compare title="同一个 worker，有没有退出路径" :panels='[{"label":"泄漏写法","code":"go func() {\n    for range ch {\n    }\n}()\n// 若 ch 永不关闭，range 永不结束","note":"阻塞路径没有终止条件，G 会一直挂在等待队列里，随请求数线性增长。"},{"label":"有退出路径","code":"go func() {\n    defer wg.Done()\n    for v := range ch {\n        handle(v)\n    }\n}()\nclose(ch)","note":"唯一的发送方结束时关闭 channel，range 自然收尾，defer 保证计数归还。"}]' note="约定「谁发送、谁关闭」，让每条阻塞路径都能被唤醒，是防泄漏最有效的一条纪律。" />

不要自己造「goroutine 池」来复用 G：语言已经把创建成本压到极低，池化只会引入排队、超时与归还逻辑，反而更容易泄漏。真正需要限制的是并发**数量**（例如同时最多 20 个外部请求），那应该用带缓冲的 channel 或信号量显式限流，而不是池化执行单元本身。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l4-concurrency/lesson-039-goroutine`](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-039-goroutine)。clone 仓库后进入 `examples/l4-concurrency`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="用 NumGoroutine 观察生命周期" task="写一个程序：先打印启动前的 goroutine 数，再启动 5 个各睡 10ms 的 goroutine，打印启动后的数量，最后用 WaitGroup 等待并打印结束后的数量。思考为什么结束后数量会回落。" :hints='["runtime.NumGoroutine() 返回当前存在的 G 总数，包含 main 自己","wg.Add(5) 可以一次加满，也可以每次循环加 1","睡 10ms 是为了让「启动后」这次采样能看到它们还活着"]' solution='package main\n\nimport (\n    "fmt"\n    "runtime"\n    "sync"\n    "time"\n)\n\nfunc main() {\n    fmt.Println("启动前:", runtime.NumGoroutine())\n    var wg sync.WaitGroup\n    wg.Add(5)\n    for i := 0; i &lt; 5; i++ {\n        go func() {\n            defer wg.Done()\n            time.Sleep(10 * time.Millisecond)\n        }()\n    }\n    fmt.Println("启动后:", runtime.NumGoroutine())\n    wg.Wait()\n    fmt.Println("结束后:", runtime.NumGoroutine())\n}' explain="启动前通常只有 main 一个 G，启动后变成 6 个，Wait 之后五个 G 的函数返回、状态变为可被复用的终止态，计数回落到 1。若结束后数字没有回落，就说明有 G 还挂在某条阻塞路径上。" />

## 随堂检测

<Quiz no="039-1" q="关于 goroutine 的栈，下面哪一项是正确的？" :options='["每个 goroutine 生来就有 8MB 固定栈，与线程一样","初始栈约 2KB，按需自动增长，函数返回后会被回收","栈只增不减，运行过的 goroutine 的栈永远不会释放","goroutine 栈由内核分配，创建必须陷入内核态"]' answer="1" explain="Go 运行时在用户态给每个 G 分配约 2KB 的初始栈，不够时自动搬去更大的连续栈；函数返回后 G 终止、栈被回收，创建过程不需要系统调用。" />

<Predict no="039-2" q="main 里用循环起了 3 个执行 fmt.Println(i) 的 goroutine，随后函数直接结束，最可能看到什么？" :options='["稳定打印 0 1 2","大概率什么都不打印","固定打印三行，只是顺序随机","编译报错：goroutine 不能被丢弃"]' answer="1" output="（大概率没有任何输出，程序直接退出）" explain="main 也是一个 goroutine，它一返回进程就结束，运行时不会等待其它 G。想让输出出现，必须用 WaitGroup、channel 或其它同步手段显式等待；这也是「起了协程却看不到日志」的头号原因。" />

## 一页纸速记

<KnowledgeCard :points='["go 关键字立刻返回，新建 G 由运行时择机执行；main 不等任何 G","初始栈约 2KB、按需增长、返回即回收，创建与切换都在用户态","G 是最小调度单位，不绑定线程；GOMAXPROCS 限制并行度而非 G 的数量","用 WaitGroup 显式等待，用 NumGoroutine 与 goroutine profile 排查堆积"]' :pitfalls='["删掉 Wait 就以为任务会自己跑完，输出莫名其妙消失","后台 G 阻塞在无人唤醒的 channel 上，形成慢性泄漏","把 GOMAXPROCS 当成 goroutine 数量上限，或盲目手搓 goroutine 池"]' summary="goroutine 轻在栈与调度，重在收尾：起得便宜，更要保证每条阻塞路径都等得到唤醒。" />

## 小结与下一讲

- `go` 语句立即返回，新 G 由运行时调度；`main` 退出会结束整个进程，必须显式等待。
- G 初始栈约 2 KB、按需增长、返回即回收，不绑定线程，十万级规模很常见；`GOMAXPROCS` 管的是并行度。
- 泄漏的根源是「永久阻塞却无人唤醒」；靠关闭约定、取消信号与 goroutine profile 防住它。

既然 G 要被「谁」挑起来执行，下一讲就把 G、M、P 三者摊开，画出完整的调度模型。

[下一讲：GMP 调度模型全景](/concurrency/gmp)
