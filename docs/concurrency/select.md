---
title: 第 044 讲 · select、超时与多路复用
description: 用 select 同时监听多个 channel，掌握随机选择、default 非阻塞、超时控制与 done channel
---

<StageHeader stage="L4 · 并发编程" level="★★★☆☆" time="40 分钟" goal="写出不会卡死的并发控制" prereq="第 043 讲 · channel 底层 hchan 与收发队列" />

# 第 044 讲 · select、超时与多路复用

> 一个 goroutine 往往要同时收数据、等超时、能被随时叫停；写成顺序接收，任何一路没数据都会卡死。select 就是「同时等待多路事件」的标准答案：像总机接线员，哪路先来接哪路，没人来时走 default 或按时离开。学完你能写出灵敏、不泄漏、不卡死的并发控制。

## 这一讲你会得到什么
<CheckList id="lesson-44" title="本讲目标" :items='["理解 select 多个 case 就绪时伪随机选择","用 default 非阻塞、用显式定时器做超时","识别 time.After 在循环里的 timer 泄漏并写出替代","用 done channel 叫停、用双层 select 做优先级"]' />

## 1. 先建立直觉
把 select 想成银行叫号屏：同时盯着几块牌子，哪块先亮处理哪块，都不亮就先去倒杯水（default）。它和 switch 形似而神异：switch 在已确定的值里挑，select 在尚未发生的通信里等。还有一条反直觉规则——多路同时就绪时不按书写顺序，而是伪随机挑一个，保证各路长期公平、不会饿死。

<ConceptDiagram type="select" title="select 多路复用" caption="数据、超时、停止三条出口，叠加计时器与 done channel 后既不空转也不失控" />

## 2. 语法与规则
先看随机选择与非阻塞。两个 channel 都放了值，前几轮命中 case，取完后命中 default，全程不阻塞。

```go
package main

import "fmt"

func main() {
	// 两个带缓冲 channel，让多个 case 同时就绪
	a := make(chan string, 2)
	b := make(chan string, 2)
	a <- "A1"
	a <- "A2"
	b <- "B1"
	b <- "B2"

	hitA, hitB := 0, 0
	for i := 0; i < 6; i++ { // 共 4 个值，后 2 轮走 default
		select {
		case v := <-a:
			fmt.Println("读 a：", v)
			hitA++
		case v := <-b:
			fmt.Println("读 b：", v)
			hitB++
		default:
			fmt.Println("都空，执行 default，不阻塞")
		}
	}
	fmt.Printf("a 命中 %d 次，b 命中 %d 次\n", hitA, hitB)
}
```

超时是 select 最常见的用法：一次调用不能无限等。注意用显式 timer 并主动 Stop。

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	// work 模拟慢任务，400ms 后回结果
	work := make(chan string, 1)
	go func() {
		time.Sleep(400 * time.Millisecond)
		work <- "任务完成"
	}()

	// 显式 NewTimer，结束时 Stop 释放底层计时器
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()

	select {
	case res := <-work:
		fmt.Println("收到结果：", res)
	case <-timer.C:
		fmt.Println("超时：100ms 内没等到结果")
	}
}
```

<OutputBox title="运行结果" output="超时：100ms 内没等到结果" note="work 要 400ms，100ms 的 timer 先就绪，稳定命中超时分支" />

显式 timer 的三步值得逐行看。

<LineByLine code="timer := time.NewTimer(d)\ndefer timer.Stop()\ntimer.Reset(d)" :notes='["NewTimer 建一次性计时器，d 到期后向 C 写入时间值","defer Stop 必须调用，否则 runtime timer 不释放，是泄漏根源","Reset 让同一 timer 重新计时，长循环应避免反复新建"]' title="显式 timer 三步" />

## 3. 深入一步
select 没有优先级。想让「停止」永远压过「任务」，做法是先做一次非阻塞探测，再进真正的多路复用。下面 worker 每轮先看 done 是否关闭，关了立刻退出。

```go
package main

import (
	"fmt"
	"time"
)

func worker(done <-chan struct{}, jobs <-chan int) {
	for {
		// 第一层：非阻塞优先检查高优先级停止信号
		select {
		case <-done:
			fmt.Println("收到停止信号，退出")
			return
		default:
		}
		// 第二层：在停止与任务之间多路复用
		select {
		case <-done:
			fmt.Println("收到停止信号，退出")
			return
		case j := <-jobs:
			fmt.Println("处理任务", j)
		}
	}
}

func main() {
	done := make(chan struct{})
	jobs := make(chan int)
	go worker(done, jobs)
	for j := 1; j <= 3; j++ {
		jobs <- j
	}
	time.Sleep(50 * time.Millisecond)
	close(done) // 关闭是广播，所有接收者立即返回
	time.Sleep(50 * time.Millisecond)
	fmt.Println("main 结束")
}
```

关键点是：关闭 channel 是广播，所有等待者立刻就绪，所以「close(done) 通知一批 goroutine」比逐个发信号更可靠。各分支行为整理成表：

| 分支形式 | 就绪条件 | 典型用途 | 风险 |
| --- | --- | --- | --- |
| 普通 case | 有值或被关闭 | 收数据、收停止信号 | 全阻塞时一直等 |
| default | 立即执行 | 非阻塞探测 | 易写成忙轮询 |
| timer.C | 计时器到期 | 超时兜底 | 循环用 time.After 会堆积 |
| done channel | 被关闭或收到值 | 统一叫停 | 忘关则下游永等 |
| nil channel | 永不就绪 | 动态禁用某路 | 需主动置 nil |

<Term word="伪随机选择" text="select 把就绪 case 随机挑一个执行，只保证公平，不保证任何顺序" />

<Callout type="warn" title="time.After 的 timer 泄漏" text="for-select 循环里反复写 time.After(d)，每次都新建一个 runtime timer。若其它 case 命中很快、间隔远小于 d，旧 timer 必须等到 d 到期才回收，短时间大量堆积会占内存、加重堆负担。高频循环改用 time.NewTimer 显式持有，配 defer Stop，需重复计时用 Reset。" />

### 再深一层：select 为什么随机、timer 为什么会堆积

<Callout type="note" title="进阶选读 · 首次学习可跳过" text="本段涉及运行时源码、内存模型与性能工具等后面讲次才系统展开的内容，属于加深理解的可选层。第一次学建议先掌握本讲主线，遇到尚未讲过的术语不必停下，学完对应讲次再回看，不影响后续理解。" />

判断一段 select 会不会出事，要落到运行时依据，而不是凭「看起来能跑」。编译器把 select 语句翻译成 `runtime.selectgo`：所有 case 的 channel 先被收进 `scases` 数组，运行时按 channel 地址排序生成 `lockorder`（固定加锁顺序，避免一次锁多个 channel 时死锁），再用 `fastrand` 打乱出 `pollorder`。所谓「伪随机选择」就来自 `pollorder`——它逐个探测就绪的 case，碰到第一个可通信的便执行，打乱是为了让各路长期公平、不被书写顺序饿死。没有任何 case 就绪时，当前 G 会为每个 channel 造一个 `sudog` 挂进 `sendq`/`recvq`（结构见第 043 讲 hchan），随后 `gopark` 挂起；任一 channel 有数据或被关闭时把它 `goready` 唤醒。由此得到两条正确性依据：被选中的那次收发与后续语句之间建立 happens-before；`close(done)` 与所有对 done 的接收之间同样建立 happens-before。所以「关闭一个 done 广播叫停一批 goroutine」在内存模型上是可靠的，而「用共享 bool 当停止标志、main 写 worker 读」没有同步，是真正的数据竞争——这一区别用 `go test -race` 一跑便知。

高频事故出在 timer 堆。`time.After(d)` 本质就是 `NewTimer(d).C`，每次调用都向运行时的 timer 堆插入一个 `runtimeTimer`（关键字段 `when` 到期纳秒、`period` 周期、`f` 回调、`arg` 参数、`seq` 序号）；Go 1.14 起每个 P 维护一棵四叉小顶堆 `timers`，到期检查由调度循环与 sysmon 承担。定时器只有到期或被 `Stop` 才从堆里摘除。当 for-select 循环里业务通道命中远快于 d 时，每轮 `time.After` 新建的 timer 都要一直挂到 d 到期才回收，短时间就能累出成千上万个对象：它不报错，只让 `HeapObjects` 与 GC 次数缓慢爬升，这就是「timer 泄漏」。想亲手复现，把下面的 `leak_test.go` 用 `go test -bench=. -benchmem -run=^$ ./...` 跑一遍，`-benchmem` 的 `allocs/op` 一列会显示 `BenchmarkTimeAfter` 每轮都新建 timer 与 channel，分配次数是 `BenchmarkTimerReuse` 的数倍；后者只 `NewTimer` 一次，命中后 `Stop` 再 `Reset`，每轮几乎零分配。示例把 d 压到 1ms 是为了让基准测试不堆积、安全跑完，真实服务里 d 往往是秒级，堆积更严重，但「每轮新建 timer」这一分配特征不变。生产定位用 `go tool pprof -alloc_objects` 找谁在调 `time.After`/`time.NewTimer`，结合 heap profile 的增长趋势，`GODEBUG=gctrace=1` 可直观看 GC 次数与堆大小。还有一条版本差异：Go 1.23 起 timer 的 channel 默认改为同步（无缓冲），`Stop` 后不再需要「从 C 兜底读一次」；旧代码若依赖缓冲行为可用 `GODEBUG=asynctimerchan=1` 恢复，但 `time.After` 每次新建 timer 的事实没变，泄漏根源仍在。

```go
package main

import (
	"testing"
	"time"
)

// 错误写法：命中很快，但每轮 time.After 都新建一个 timer，旧 timer 要等到 d 到期才回收
func loopAfter(fast chan struct{}, n int) {
	for i := 0; i < n; i++ {
		select {
		case <-fast:
			fast <- struct{}{}
		case <-time.After(time.Millisecond):
		}
	}
}

// 正确写法：只建一个 timer，命中后 Stop 再 Reset，反复复用
func loopReuse(fast chan struct{}, n int) {
	timer := time.NewTimer(time.Millisecond)
	defer timer.Stop()
	for i := 0; i < n; i++ {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(time.Millisecond)
		select {
		case <-fast:
			fast <- struct{}{}
		case <-timer.C:
		}
	}
}

func BenchmarkTimeAfter(b *testing.B) {
	fast := make(chan struct{}, 1)
	fast <- struct{}{}
	b.ResetTimer()
	loopAfter(fast, b.N)
}

func BenchmarkTimerReuse(b *testing.B) {
	fast := make(chan struct{}, 1)
	fast <- struct{}{}
	b.ResetTimer()
	loopReuse(fast, b.N)
}
```

```bash
go test -bench=. -benchmem -run=^$ ./...   # 看 allocs/op：TimeAfter 明显更高
go test -race ./...                          # 顺带确认整个包没有数据竞争
```

<OutputBox title="本机实测（Go 1.23.8 / darwin-arm64）" output="BenchmarkTimeAfter-12：3 allocs/op，248 B/op；BenchmarkTimerReuse-12：0 allocs/op，0 B/op" note="allocs/op 一列直接反映每轮新建 timer 的分配；数值随机器与版本浮动，但差距稳定" />

把上面两类失效与对应的检测、修复整理成表，排障时按行对照即可：

| 失败场景 | 根因 | 可验证的检测命令 | 修复要点 |
| --- | --- | --- | --- |
| for-select 循环里堆 timer | `time.After(d)` 每轮新建 `runtimeTimer`，命中快于 d 时到期前不回收 | `go test -bench=. -benchmem` 看 `allocs/op`；`go tool pprof -alloc_objects` | `time.NewTimer` 一次 + `defer Stop` + `Reset` 复用 |
| 用共享 bool 当停止标志 | main 写、worker 读无同步，构成数据竞争 | `go test -race ./...` 报 `WARNING: DATA RACE` 指向该变量 | 改用 `close(done)`，关闭与接收建立 happens-before |
| 阻塞等待没有超时或停止出口 | 下游卡住则 G 永久 `gopark` | `runtime.NumGoroutine()` 随请求数上涨；`go tool pprof` goroutine profile | 每个可能阻塞的 select 都配 `timer.C` 与 `done` |
| `default` 写成忙轮询 | 空 select 反复立即返回，占满一个 P | `go tool pprof` CPU profile 见该函数自环 | 无就绪时退避 `time.Sleep`，或去掉 `default` 改阻塞 |

下面这张时序图把「命中快、timer 堆积、close(done) 广播」三条线索串在一次循环里，配合本讲前面的双层 select 一起看：

<SequenceDiagram title="for-select 超时与停止链路" caption="命中远快于超时时旧 timer 滞留，close(done) 一次广播唤醒全部等待者" :actors='["主协程","worker","业务通道","timer.C","done 通道"]' :steps='[{"from":"主协程","to":"worker","label":"启动循环","note":"三路出口同时监听","kind":"call"},{"from":"worker","to":"业务通道","label":"select 等待","note":"带 default 非阻塞探测","kind":"call"},{"from":"worker","to":"timer.C","label":"注册超时","note":"time.After 每轮新建 timer","kind":"call"},{"from":"业务通道","to":"worker","label":"命中返回","note":"命中远快于 d","kind":"return"},{"from":"timer.C","to":"worker","label":"旧 timer 挂起","note":"到期前一直留在堆","kind":"async"},{"from":"worker","to":"timer.C","label":"再建 timer","note":"下一轮重复堆积","kind":"call"},{"from":"主协程","to":"done 通道","label":"close(done)","note":"一次关闭","kind":"call"},{"from":"done 通道","to":"worker","label":"广播唤醒","note":"close 建立 happens-before","kind":"return"},{"from":"worker","to":"主协程","label":"退出循环","note":"defer Stop 释放计时器","kind":"return"}]' />

<Quiz q="for-select 循环里业务通道命中远快于超时 d，反复写 time.After(d) 会带来什么后果？" :options='["timer 命中更快，程序更省内存","每轮新建的 runtimeTimer 要等到 d 到期才回收，堆对象与 GC 次数上涨","select 会退化为按书写顺序执行","编译直接报错"]' answer="1" explain="time.After 每次新建 timer 插入 per-P 的 timer 堆，命中快时旧 timer 到期前不摘除；用 go test -bench=. -benchmem 看 allocs/op，或 pprof -alloc_objects 定位调用者，修法是 NewTimer 一次加 Stop 加 Reset 复用。" />

<Callout type="warn" title="别把 Go 1.23 的同步 timer 当成『不用 Stop』" text="1.23 把 timer channel 改为无缓冲后，Stop 之后『再从 C 兜底读一次』的旧套路不再必要；但 NewTimer、NewTicker 持有的 runtime timer 仍要 Stop 才会从堆摘除。高频循环里只复用不 Stop、或反复 time.After，泄漏照旧。判断标准始终是：这个等待有没有超时或停止出口，timer 有没有被显式释放。" />

## 4. 工程实践
真实服务里 select 几乎总是「数据 + 超时 + 停止」三件套：HTTP handler 等下游结果、等 deadline、等客户端断开；后台 worker 抢任务、等心跳、等优雅退出。经验是任何可能阻塞的等待都要配超时或停止出口，否则一个卡住的下游就能拖垮整组 goroutine；定时器复用而非每次新建，done 用 close 广播。

<Compare :panels='[{"label":"写法 A：循环里直接用","code":"time.After(d)","note":"每次新建 timer，命中快时旧 timer 堆积到 d 到期才回收"},{"label":"写法 B：显式持有复用","code":"timer := time.NewTimer(d); defer timer.Stop(); timer.Reset(d)","note":"只建一次，Stop 主动释放，Reset 复用，是长循环正确写法"}]' title="超时写法的取舍" />

### 与 L10 实战的呼应

select 的「数据 + 超时 + 停止」三件套在后面的实战里几乎是同一套骨架。第 109 讲 [并发爬虫](/projects/concurrent-crawler) 的 Fetcher worker 每轮都在 select 里同时等任务、等单域名限速令牌、等 `http.Client` 的整体超时与 `ctx.Done()`，任何一路先到就收手，正是本讲「多路复用加显式 timer 加 done 广播」的生产版本；循环里若用 `time.After` 堆 timer，抓取高峰会直接把堆对象数拉起来。第 110 讲 [分布式任务调度](/projects/concurrent-scheduler) 的优雅退位用 close 广播叫停一批在途任务、用租约 TTL 做超时释放，Master 换主窗口里「谁先超时」要靠 select 判定。第 116 讲 [秒杀与高并发库存](/projects/micro-seckill) 的结果轮询与排队超时释放，同样要求每个等待都有 `timer.C` 与 `done` 两条出口，否则洪峰里一个卡住的下游就能拖垮整组 goroutine。第 118 讲 [LLM 网关](/projects/ai-gateway) 把「首 token 超时、整体 deadline、客户端断开」并进一次 SSE 流式转发的 select，是这套三件套最贴近当下的用法。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l4-concurrency/lesson-044-select`](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-044-select)。clone 仓库后进入 `examples/l4-concurrency`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="让定时任务干净停下" task="用 time.NewTicker 每 10ms 打印一次，累计 3 次后退出并释放定时器，要求 defer 释放、用 for range 接收。" :hints='["ticker.C 可用 for range 接收","defer ticker.Stop() 不能漏","用计数变量达到 3 就 return"]' solution="计数器 := 0\nticker := time.NewTicker(10 * time.Millisecond)\ndefer ticker.Stop()\nfor range ticker.C {\n    计数器 = 计数器 + 1\n    fmt.Println(计数器)\n    if 计数器 == 3 {\n        return\n    }\n}" explain="Ticker.C 是只读 channel，for range 阻塞式接收；计数到 3 时 return，defer 保证 Stop 释放 runtime timer。这正是替代循环里不断 time.After 的思路。" />

## 随堂检测
<Quiz no="044-1" q="select 有多个 case 同时就绪时，Go 会怎么做？" :options='["按书写顺序选第一个","伪随机挑一个就绪 case","选缓冲最大的那个","编译报错"]' answer="1" explain="运行时把就绪 case 收集后伪随机选一个，保证各路长期公平，避免固定顺序让某些通道饿死。" />
<Predict no="044-2" q="两个 channel 都为空、select 带 default，结果是？" :options='["阻塞直到有数据","立刻执行 default","因全阻塞而 panic","编译失败"]' answer="1" output="立刻执行 default，select 不阻塞" explain="没有 case 就绪时 default 立即执行，这是实现非阻塞收发、避免空等的关键。" />

## 一页纸速记
<KnowledgeCard :points='["多个 case 就绪时伪随机选，不保证顺序","default 让 select 永不阻塞","超时用 time.NewTimer 加 defer Stop，别反复 time.After","close(done) 广播唤醒所有接收者","优先级靠带 default 的非阻塞探测"]' :pitfalls='["循环里 time.After 堆积 timer 泄漏","阻塞等待没配超时或停止出口","误以为 select 按书写顺序执行"]' summary="select 把多路通信、超时与停止并成一次等待：谁先就绪走谁，没人就绪走 default，到期走 timer" />

## 小结与下一讲

- select 多路复用：就绪 case 伪随机选一个，default 提供非阻塞出口。
- 超时用显式 timer 并主动 Stop，循环滥用 time.After 会累积 timer；close(done) 广播叫停。
- 优先级靠双层 select，任何阻塞等待都要留超时或停止出口。

[下一讲：sync：Mutex / RWMutex / WaitGroup / Once / Cond](/concurrency/sync-primitives)
