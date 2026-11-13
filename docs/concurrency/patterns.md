---
title: 第 048 讲 · 并发模式：pipeline / fan-out / worker pool
description: 把 pipeline、扇出扇入、worker pool、errgroup、singleflight 与限速背压写成可复用的并发套路
---

<StageHeader stage="L4 · 并发编程" level="★★★★☆" time="50 分钟" goal="把常见并发结构写成可复用套路" prereq="第 047 讲 · context：取消、超时、值传递" />

# 第 048 讲 · 并发模式：pipeline / fan-out / worker pool

> 前面给了积木，真实业务却要把数据流组织成稳定的生产线。本讲把积木拼成六种高频套路：阶段管道、扇出扇入、worker pool、errgroup、singleflight、限速背压，每种都给出可编译实现。

## 这一讲你会得到什么
<CheckList id="lesson-48" title="本讲目标" :items='["用 channel 把处理拆成可组合的 pipeline 阶段","写出完整的 fan-out、fan-in 与 worker pool","用标准库实现带首个错误返回的迷你 errgroup","理解 singleflight 与限速背压的适用场景"]' />

## 1. 先建立直觉
把并发程序想成灌装线：generator 洗瓶，square 灌装，主协程装箱，工位间用 channel 连接；瓶子满了上游停下等，就是背压。提速多开支路（扇出）最后汇回主带（扇入）。worker pool 是「固定几人共用待办传送带」，errgroup 是「谁出事全员停工」，singleflight 是「同款饮料只做一杯大家分」。

<ConceptDiagram type="pipeline" title="管道、扇出与扇入的数据流" caption="阶段用 channel 连接，扇出把任务分给多 worker，扇入把结果汇回一处，末端可加限速" />

## 2. 语法与规则
先搭管道。每个阶段都是「读只读 channel、写只写 channel、完成后关闭输出」的函数。关不关闭 channel 是收尾关键：上游不关，下游 range 永不结束。

```go
package main

import "fmt"

// gen 生产阶段：送数后关闭
func gen(nums ...int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out) // 必须关闭，否则下游 range 不结束
		for _, n := range nums {
			out <- n
		}
	}()
	return out
}

// square 加工阶段：读 in、平方后写 out
func square(in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for n := range in { // in 关闭时自动退出
			out <- n * n
		}
	}()
	return out
}

func main() {
	// 串成管道：gen 接 square，main 消费输出
	for s := range square(gen(1, 2, 3, 4, 5)) {
		fmt.Println("平方：", s)
	}
}
```

再看扇出、扇入与 worker pool 的完整实现：多 worker 共享 jobs（扇出），各独占一个输出通道，由 merge 汇成一条（扇入）。

```go
package main

import (
	"fmt"
	"sync"
)

// worker 从 jobs 取任务，结果写进自己独占的 out
func worker(id int, jobs <-chan int, out chan<- int) {
	defer close(out) // 独占输出，结束时关闭
	for j := range jobs {
		out <- j * j
		_ = id
	}
}

// merge 扇入：把多个只读通道汇成一个
func merge(cs ...<-chan int) <-chan int {
	var wg sync.WaitGroup
	merged := make(chan int)
	for _, c := range cs {
		wg.Add(1)
		go func(ch <-chan int) {
			defer wg.Done()
			for n := range ch {
				merged <- n
			}
		}(c)
	}
	go func() {
		wg.Wait()
		close(merged) // 所有输入关闭后才关汇合通道
	}()
	return merged
}

func main() {
	jobs := make(chan int)
	go func() {
		defer close(jobs)
		for j := 1; j <= 8; j++ {
			jobs <- j
		}
	}()

	const workers = 4 // 固定数量即 pool 容量
	outs := make([]<-chan int, 0, workers)
	for i := 0; i < workers; i++ {
		out := make(chan int)
		outs = append(outs, out) // 双向可赋给只读
		go worker(i, jobs, out)
	}
	sum := 0
	for n := range merge(outs...) {
		sum += n
	}
	fmt.Println("平方和 =", sum)
}
```

<OutputBox title="运行结果" output="平方和 = 204" note="1 到 8 平方和为 204；worker 数只影响并行度，close 与 WaitGroup 保证不丢任务" />

worker pool 的汇合骨架值得逐行看。

<LineByLine code="wg.Add(1)\ngo func() { defer wg.Done(); work() }()\nwg.Wait()" :notes='["Add 在启动前登记，计数必须先于 goroutine","启动 goroutine，defer Done 保证正常或出错都计数","Wait 阻塞到归零，是所有 worker 结束的汇合点"]' title="worker pool 的汇合点" />

## 3. 深入一步
errgroup 的价值是「编组 + 首错取消」：`golang.org/x/sync/errgroup` 的 Go 收集错误，WithContext 首错取消其余，SetLimit 限并发。下面用标准库写个迷你版，实现并发上限（背压）与首个错误。

```go
package main

import (
	"fmt"
	"sync"
)

// runAll 模拟 errgroup：带并发上限，返回首个错误
func runAll(tasks []int, limit int) error {
	sem := make(chan struct{}, limit) // 信号量即背压
	errCh := make(chan error, len(tasks))
	var wg sync.WaitGroup

	for _, t := range tasks {
		sem <- struct{}{} // 达上限就阻塞，直到有人释放
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			defer func() { <-sem }() // 完成一个释放一个
			if n == 3 {
				errCh <- fmt.Errorf("任务 %d 执行失败", n)
				return
			}
			fmt.Println("完成任务", n)
		}(t)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh { // 只取第一个错误
		return err
	}
	return nil
}

func main() {
	if err := runAll([]int{1, 2, 3, 4, 5}, 2); err != nil {
		fmt.Println("首个错误：", err)
	}
}
```

singleflight 是另一种思路：合并「同一 key 的重复调用」，让昂贵加载只真正执行一次，结果由所有并发调用者共享，典型用途是防缓存击穿，同样来自 x/sync，标准库没有内置。六种套路对比：

| 模式 | 解决什么 | 关键手段 | 主要风险 |
| --- | --- | --- | --- |
| pipeline | 拆成可组合阶段 | 阶段返回只读 channel，逐段 defer close | 忘关 channel，range 永不结束 |
| fan-out | 多核并行加速 | 多 worker 共享输入 | 工人过多打垮下游 |
| fan-in | 汇总多路结果 | merge 用 WaitGroup 等齐再关输出 | 过早关闭汇合通道丢数据 |
| worker pool | 固定并发复用 | 固定 goroutine 加 jobs 队列 | 队列无界内存失控 |
| errgroup | 编组、首错取消 | Go 收集错误，WithContext 取消 | 忘 Wait 或忽略错误 |
| singleflight | 合并重复请求 | Do 保证同 key 只跑一次 fn | 不同参数错用同一 key |

<Term word="背压" text="下游处理不过来时，用有界 channel 或信号量让上游阻塞，把压力反向传导、限制在途任务量" />

<Callout type="warn" title="收尾比启动更易错" text="管道事故多出在收尾：生产阶段忘 defer close，消费方 range 永远阻塞、goroutine 泄漏；fan-in 还有输入没关就关汇合通道会丢数据甚至 panic。约定「谁写谁关」：每个 goroutine 只关自己独占的输出，汇合方等齐再关总输出。" />

<Callout type="tip" title="二者来自 x/sync" text="二者都在 golang.org/x/sync，需 go get 引入。errgroup.WithContext 首错关联 cancel 让兄弟任务收手，生产环境比迷你版更值得用；singleflight.Group.Do 适合「缓存失效瞬间大量回源」去重。都要透传 ctx。" />

### 再深一层：close 顺序、背压与「收尾比启动更易错」

<Callout type="note" title="进阶选读 · 首次学习可跳过" text="本段涉及运行时源码、内存模型与性能工具等后面讲次才系统展开的内容，属于加深理解的可选层。第一次学建议先掌握本讲主线，遇到尚未讲过的术语不必停下，学完对应讲次再回看，不影响后续理解。" />

管道的正确性几乎全押在收尾上，而收尾的底层依据是 channel 的阻塞与唤醒规则。编译期 `for n := range ch` 被展开成反复调用 `chanrecv` 的循环：只有当通道「已关闭且缓冲区排空」时，`chanrecv` 才返回 `ok=false` 让循环结束；否则接收方 G 会被封成一个 `sudog` 挂进 `hchan.recvq`，由 `gopark` 挂起，等某个发送或 `close` 来 `goready` 它。因此生产阶段漏写 `defer close(out)`，下游 G 就永远等不到唤醒——这就是 goroutine 泄漏，而且 `go test -race` 对此完全无感（它只查数据竞争）。反过来，`close(ch)` 会一次性唤醒 `sendq`/`recvq` 里的所有 G：对已关闭通道再发送会 panic `send on closed channel`，所以纪律是「谁写谁关」，消费方和 fan-in 汇合方绝不能抢着关。本讲 `merge` 里先 `wg.Wait()` 等所有输入 goroutine 结束、再 `close(merged)`，正是为了避开「还有人写就关」的丢数据与 panic。

把这段推理做成可复现实验：`genOK` 用 `defer close(out)`，`TestPipelineClose` 的 `for range` 顺利收完 1、2、3 并断言和为 6；`genNoClose` 故意漏关，`TestNoCloseLeaks` 让一个消费 goroutine `for range ch`，50ms 后 `done` 仍没关闭，于是超时分支成立——这就是「range 永不结束」的实证。两条命令分别跑 `go test -race -run=TestPipelineClose -v ./...` 与 `go test -run=TestNoCloseLeaks -v ./...`，对照一目了然。注意 `-race` 在这里的作用只是确认无数据竞争，抓泄漏要靠 `runtime.NumGoroutine()` 前后对比、`go.uber.org/goleak` 在测试收尾校验，或 `go tool pprof` 的 goroutine profile 里那些卡在 `chanrecv`/`semacquire` 的 G。

背压与错误收尾是同一套思路的延伸。有界 channel 或信号量 `sem := make(chan struct{}, limit)` 在满时让 `sem <- struct{}{}` 阻塞，把在途任务数压在 limit 内，避免上游灌爆内存；`GODEBUG=gctrace=1` 能直观看堆是否随任务数线性上涨，配合 `go tool pprof -alloc_space` 定位无界队列。`errgroup.WithContext` 则是把「首错」变成一次 `cancel`：某个 `Go` 返回非 nil 错误，关联 ctx 立刻取消，兄弟任务在 `ctx.Done()` 上收手，避免慢依赖拖死整个请求。生产里把这些叠加起来——ETL 用 pipeline 分段、转换扇出到核数、有界 channel 背压、errgroup 出错取消全流程——再用 `-race` 与压力多次运行验证 close 顺序，才算是把「能跑」变成「收得干净」。补一条运行时依据：`close(ch)` 并非普通赋值，它由 `runtime.closechan` 把 `recvq` 里排队的 `sudog` 依次 `goready` 唤醒，这正是「一次关闭广播全部接收者」既能替代逐个发信号、又天然带 happens-before 的原因；反过来对已关闭通道发送会 panic，所以发送方才必须独占关闭权。

```go
package main

import (
	"testing"
	"time"
)

// 错误写法：生产阶段忘了 close(out)，消费方 range 永远阻塞
func genNoClose(nums []int) <-chan int {
	out := make(chan int)
	go func() {
		for _, n := range nums {
			out <- n
		}
		// 漏掉 close(out)
	}()
	return out
}

// 正确写法：defer close(out)，下游 range 才能结束
func genOK(nums []int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for _, n := range nums {
			out <- n
		}
	}()
	return out
}

func TestPipelineClose(t *testing.T) {
	total := 0
	for n := range genOK([]int{1, 2, 3}) {
		total += n
	}
	if total != 6 {
		t.Fatalf("期望 6，实际 %d", total)
	}
}

func TestNoCloseLeaks(t *testing.T) {
	ch := genNoClose([]int{1, 2, 3, 4, 5})
	done := make(chan struct{})
	go func() {
		for range ch { // 永远收不到关闭信号
		}
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("不该结束：漏关 channel 时 range 应永久阻塞")
	case <-time.After(50 * time.Millisecond):
		// 预期：消费 goroutine 泄漏，可用 runtime.NumGoroutine 或 goleak 观察
	}
}
```

```bash
go test -race -run=TestPipelineClose -v ./...   # 正确收尾：和为 6
go test -run=TestNoCloseLeaks -v ./...           # 反面实验：range 永不结束
go test -race ./...                              # 全量回归
```

把收尾链路上的典型失效整理成表，代码评审时按「谁关、何时关、漏了怎么测」逐行核对：

| 收尾动作 | 遗漏后果 | 可验证的检测命令 | 修复要点 |
| --- | --- | --- | --- |
| 生产阶段忘 `close(out)` | 下游 `range` 永不结束，G 挂在 recvq 泄漏 | `go test -run=TestNoCloseLeaks -v ./...`；`runtime.NumGoroutine()` 上涨；goroutine profile | 每个产生输出的 goroutine `defer close(out)` |
| 消费方或汇合方提前 `close` | 仍可能有生产者写入，触发 send on closed channel panic | `go test -race ./...` 复跑；panic 栈定位 | 「谁写谁关」，fan-in 等 `wg.Wait()` 齐了再关 |
| fan-in 输入没关就关 merged | 丢数据或提前结束 | 断言总和（如 204）；压测多次跑 | merge 里 `wg.Wait()` 后再 `close(merged)` |
| 无界 jobs 队列或无限速 | 上游灌爆内存，下游被压垮 | `GODEBUG=gctrace=1` 看堆增长；`go tool pprof -alloc_space` | 有界 channel 加信号量形成背压 |
| errgroup 忽略返回错误 | 首错不取消其余，慢依赖继续跑 | 注入错误任务并计时 | `g.Wait()` 后必判错，配 WithContext 首错取消 |

下图把「阶段、有界 channel、谁写谁关、末端限速」放在同一张流图里，配合前面的 merge 与 runAll 示例看收尾顺序：

<ConceptDiagram type="pipeline" title="带背压与收尾顺序的管道" caption="阶段间用有界 channel 传递，谁写谁关；fan-in 等齐再关汇合通道，末端加信号量限速" />

<Quiz q="pipeline 的生产阶段漏写 defer close(out)，下游用 for range 接收会怎样？" :options='["range 立即结束","编译报错","下游 G 永久阻塞在 recvq，发生 goroutine 泄漏","自动收到零值"]' answer="2" explain="range 只在通道关闭且排空后才退出；未关闭时 chanrecv 把 G 挂进 hchan.recvq 并 gopark，永不被唤醒。TestNoCloseLeaks 用 50ms 超时证明 done 永远不 close；-race 抓不到泄漏，要用 runtime.NumGoroutine 或 goroutine profile。" />

<Callout type="tip" title="-race 抓不到 goroutine 泄漏" text="race detector 只报数据竞争，漏关 channel、忘记 cancel、卡在无出口的 select 这类泄漏它全都放过。工程上把 go test 前后对比 runtime.NumGoroutine()、用 go.uber.org/goleak 在测试收尾校验、或看 go tool pprof 的 goroutine profile 里卡在 chanrecv 与 semacquire 的 G，才是定位手段。" />

## 4. 工程实践
真实系统常叠加使用：ETL 用 pipeline 串起「读取、清洗、转换、写出」，转换扇出到 CPU 核数的 worker，有界 channel 背压，errgroup 出错取消全流程。批量调 RPC 时 errgroup 加超时避免慢依赖拖死请求；热点 key 回源用 singleflight 把「千个并发」压成「一次」。限速配合 context，最后用 -race 跑。

<Compare :panels='[{"label":"写法 A：不设并发上限","code":"g.Go(func() error { return handle(t) })","note":"任务一次性并发，可能打垮数据库"},{"label":"写法 B：SetLimit 限速","code":"g.SetLimit(8); g.Go(func() error { return handle(t) })","note":"errgroup 把在跑任务压在 8 个内，形成背压"}]' title="并发要不要设上限" />

### 与 L10 实战的呼应

本讲的六种套路几乎就是后面实战的目录。第 109 讲 [并发爬虫](/projects/concurrent-crawler) 的 V2 单机并发版是标准的 worker pool 加有界 `jobs` 队列，V3 分布式版把 Frontier、Fetcher、Parser、Result Store 拆成 pipeline 阶段，扇出到多 worker、扇入汇总结果，队列满时背压到生产端——「谁写谁关」「fan-in 等齐再关」正是本讲收尾纪律的放大版。第 107 讲 [日志分析 ETL](/projects/data-log-etl) 用 pipeline 串起逐行读取、清洗、窗口统计与落库，转换阶段扇出到 CPU 核数、`errgroup` 出错取消全流程。第 110 讲 [分布式任务调度](/projects/concurrent-scheduler) 的 Master 分片派发是 fan-out、Worker 回报汇合是 fan-in，执行器隔离则是 worker pool 的固定槽位。第 116 讲 [秒杀与高并发库存](/projects/micro-seckill) 用限流排队加有界队列削峰，`singleflight` 则常用于热点 key 回源去重、防缓存击穿。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l4-concurrency/lesson-048-patterns`](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-048-patterns)。clone 仓库后进入 `examples/l4-concurrency`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="用 WaitGroup 汇合一组 worker" task="把 tasks 每项交给一个 goroutine 处理，用 WaitGroup 等全部结束再继续，要求 Add 在启动前、Done 用 defer。" :hints='["循环里先 wg.Add(1)","函数体首行 defer wg.Done()","循环结束调用 wg.Wait()"]' solution="var wg sync.WaitGroup\nfor _, t := range tasks {\n    wg.Add(1)\n    go func(x int) {\n        defer wg.Done()\n        fmt.Println(x)\n    }(t)\n}\nwg.Wait()" explain="worker pool 最小汇合骨架：Add 登记、defer Done 计数、Wait 等待。循环变量通过参数 x 传入，避免闭包捕获同一变量；正式项目再叠加 jobs 队列与 context。" />

## 随堂检测
<Quiz no="048-1" q="关于 errgroup 与 singleflight，下面说法正确的是？" :options='["errgroup 合并相同 key 的重复请求","singleflight 让首错取消其它任务","两者都在标准库 sync 包里","errgroup 编组任务，任一出错即可统一取消其余"]' answer="3" explain="errgroup 编组 goroutine 并在首错时取消其余；singleflight 合并同 key 调用防击穿；二者都来自 golang.org/x/sync。" />
<Predict no="048-2" q="把 1 到 8 交给 4 个 worker 各自平方再汇总，平方和是？" :options='["204","36","不确定取决于调度","会死锁"]' answer="0" output="平方和 = 204" explain="1 到 8 平方和为 204；worker 数只改并行度，close 与 WaitGroup 保证每任务恰好处理一次。" />

## 一页纸速记
<KnowledgeCard :points='["pipeline 每阶段返回只读 channel，谁写谁关","fan-out 共享输入，fan-in 用 merge 汇总","worker pool 固定数量配 jobs 队列","errgroup 首错取消，singleflight 合并调用","有界 channel 与 SetLimit 形成背压"]' :pitfalls='["生产阶段忘关 channel，range 永挂","fan-in 过早关汇合通道丢数据","无限速起 goroutine 压垮下游"]' summary="拆成阶段、扇出扇入并行、pool 与限速控规模、errgroup 与 singleflight 处理错误与去重" />

## 小结与下一讲

- pipeline 靠「每阶段返回只读通道 + 谁写谁关」串联，fan-out 分活、fan-in 汇总、pool 固定并发。
- errgroup 首错统一取消，singleflight 合并同 key 重复调用，二者来自 x/sync 而非标准库。
- 有界 channel 提供背压，close 顺序比启动更易错，务必配合 context 与 -race。

[下一讲：数据竞争、内存模型与 race detector](/concurrency/race)
