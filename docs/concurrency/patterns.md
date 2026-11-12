---
title: 第 48 讲 · 并发模式：pipeline / fan-out / worker pool
description: 把 pipeline、扇出扇入、worker pool、errgroup、singleflight 与限速背压写成可复用的并发套路
---

<StageHeader stage="L4 · 并发编程" level="★★★★☆" time="50 分钟" goal="把常见并发结构写成可复用套路" prereq="第 47 讲 · context：取消、超时、值传递" />

# 第 48 讲 · 并发模式：pipeline / fan-out / worker pool

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

## 4. 工程实践
真实系统常叠加使用：ETL 用 pipeline 串起「读取、清洗、转换、写出」，转换扇出到 CPU 核数的 worker，有界 channel 背压，errgroup 出错取消全流程。批量调 RPC 时 errgroup 加超时避免慢依赖拖死请求；热点 key 回源用 singleflight 把「千个并发」压成「一次」。限速配合 context，最后用 -race 跑。

<Compare :panels='[{"label":"写法 A：不设并发上限","code":"g.Go(func() error { return handle(t) })","note":"任务一次性并发，可能打垮数据库"},{"label":"写法 B：SetLimit 限速","code":"g.SetLimit(8); g.Go(func() error { return handle(t) })","note":"errgroup 把在跑任务压在 8 个内，形成背压"}]' title="并发要不要设上限" />

## 动手练一练
<Exercise title="用 WaitGroup 汇合一组 worker" task="把 tasks 每项交给一个 goroutine 处理，用 WaitGroup 等全部结束再继续，要求 Add 在启动前、Done 用 defer。" :hints='["循环里先 wg.Add(1)","函数体首行 defer wg.Done()","循环结束调用 wg.Wait()"]' solution="var wg sync.WaitGroup\nfor _, t := range tasks {\n    wg.Add(1)\n    go func(x int) {\n        defer wg.Done()\n        fmt.Println(x)\n    }(t)\n}\nwg.Wait()" explain="worker pool 最小汇合骨架：Add 登记、defer Done 计数、Wait 等待。循环变量通过参数 x 传入，避免闭包捕获同一变量；正式项目再叠加 jobs 队列与 context。" />

## 随堂检测
<Quiz no="48-1" q="关于 errgroup 与 singleflight，下面说法正确的是？" :options='["errgroup 合并相同 key 的重复请求","singleflight 让首错取消其它任务","两者都在标准库 sync 包里","errgroup 编组任务，任一出错即可统一取消其余"]' answer="3" explain="errgroup 编组 goroutine 并在首错时取消其余；singleflight 合并同 key 调用防击穿；二者都来自 golang.org/x/sync。" />
<Predict no="48-2" q="把 1 到 8 交给 4 个 worker 各自平方再汇总，平方和是？" :options='["204","36","不确定取决于调度","会死锁"]' answer="0" output="平方和 = 204" explain="1 到 8 平方和为 204；worker 数只改并行度，close 与 WaitGroup 保证每任务恰好处理一次。" />

## 一页纸速记
<KnowledgeCard :points='["pipeline 每阶段返回只读 channel，谁写谁关","fan-out 共享输入，fan-in 用 merge 汇总","worker pool 固定数量配 jobs 队列","errgroup 首错取消，singleflight 合并调用","有界 channel 与 SetLimit 形成背压"]' :pitfalls='["生产阶段忘关 channel，range 永挂","fan-in 过早关汇合通道丢数据","无限速起 goroutine 压垮下游"]' summary="拆成阶段、扇出扇入并行、pool 与限速控规模、errgroup 与 singleflight 处理错误与去重" />

## 小结与下一讲

- pipeline 靠「每阶段返回只读通道 + 谁写谁关」串联，fan-out 分活、fan-in 汇总、pool 固定并发。
- errgroup 首错统一取消，singleflight 合并同 key 重复调用，二者来自 x/sync 而非标准库。
- 有界 channel 提供背压，close 顺序比启动更易错，务必配合 context 与 -race。

[下一讲：数据竞争、内存模型与 race detector](/concurrency/race)
