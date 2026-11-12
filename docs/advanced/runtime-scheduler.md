---
title: 第 95 讲 · 调度器源码：schedule / findRunnable / sysmon
description: 把 GMP 从概念落到代码，读懂调度主循环、取 G 顺序、工作窃取与异步抢占
---

<StageHeader stage="L9 · 资深进阶与源码" level="★★★★★" time="60 分钟" goal="把 GMP 从概念落到代码" prereq="第 41 讲 · 调度循环：抢占、窃取与 handoff" />

# 第 95 讲 · 调度器源码：schedule / findRunnable / sysmon

> 第 40 讲画过 GMP 的关系图，第 41 讲讲过抢占与窃取的思路，但那些都是模型。真正决定「下一刻哪个 goroutine 上 CPU」的，是 runtime/proc.go 里不到百行的 schedule 主循环，以及它调用的 findRunnable。本讲把模型对回代码：看签名、看字段、看取 G 的优先顺序，再用 `GODEBUG=schedtrace` 与可运行程序把结论验证一遍。源码以 Go 1.23.8 为准。

## 这一讲你会得到什么
<CheckList id="L9-95" title="本讲目标" :items='["复述 schedule 主循环与 findRunnable 的取 G 顺序", "说清本地队列、全局队列与工作窃取各自的角色", "用 schedtrace 与紧循环程序验证 sysmon 的异步抢占"]' />

## 1. 先建立直觉
把每个 P 想成一个带着传送带的小车间：传送带是本地运行队列 runq，车间门口还有一排公共货架（全局队列）。工人 M 每次从自己传送带头部取活，取不到才去公共货架，货架也空了就去别的车间「顺」一半过来。sysmon 是巡厂员，它不干活，只盯着有没有车间卡住太久或有人独占机器超过时间片。于是调度器的目标可以浓缩成一句话：别让任何 P 闲着，也别让任何 G 饿死。
<ConceptDiagram type="scheduler-loop" title="schedule 主循环的取 G 顺序" caption="先本地、再全局、后窃取；拿不到才让 M 进入休眠" />

## 2. 语法与规则
先定位关键函数与字段，再用程序验证行为。以下路径均来自 Go 1.23.8 的 GOROOT，行号仅作索引。

- `src/runtime/proc.go:3967`：`func schedule()`，每次调度都从 `top:` 标签开始重新挑选 G。
- `src/runtime/proc.go:3250`：`func findRunnable() (gp *g, inheritTime, tryWakeP bool)`，返回下一个可运行的 G。
- `src/runtime/proc.go:6820 / 6528 / 6941`：`runqget`、`globrunqget`、`runqsteal`。
- `src/runtime/proc.go:6044`：`func sysmon()`，独立于 P 的监控线程；同文件 6200 行定义 `const forcePreemptNS = 10 * 1000 * 1000`，即 10ms 时间片。
- `src/runtime/runtime2.go:650`：`type p struct` 含 `runqhead`、`runqtail`、`runq [256]guintptr` 与 `runnext guintptr`；776 行的 `type schedt struct` 含全局 `runq gQueue`。

三段程序分别验证 P 的数量与 G 的分布、异步抢占、以及本地队列容量溢出。

```go
package main

import (
	"fmt"
	"runtime"
	"sync/atomic"
)

// 观察一：GOMAXPROCS 决定并行度，NumGoroutine 统计当前存活的 G。
func main() {
	fmt.Println("默认 P 数 =", runtime.GOMAXPROCS(0))
	runtime.GOMAXPROCS(2)
	fmt.Println("调整后 P 数 =", runtime.GOMAXPROCS(0))

	var counter int64
	const n = 8
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			atomic.AddInt64(&counter, 1)
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}
	fmt.Println("完成计数 =", atomic.LoadInt64(&counter))
	fmt.Println("收尾后 G 数 =", runtime.NumGoroutine())
}
```

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

// 观察二：无函数调用的紧循环占满单个 P，验证 sysmon 的异步抢占。
// Go 1.14 起，纯计算循环也能被抢占，main 不会被永久卡住。
var sink int64

func main() {
	runtime.GOMAXPROCS(1)
	done := make(chan struct{})
	go func() {
		var x int64
		for i := int64(0); i < 2_000_000_000; i++ {
			x += i % 7
		}
		sink = x
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("后台循环已结束")
	case <-time.After(100 * time.Millisecond):
		fmt.Println("100ms 内 main 仍被调度：异步抢占生效")
		fmt.Println("P 数量 =", runtime.GOMAXPROCS(0))
	}
}
```

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
)

// 观察三：本地队列 runq 容量是 256，一次就绪超过这个数量，
// 多余的 G 会溢出到 schedt.runq 全局队列，由 schedtrace 可见。
func main() {
	fmt.Println("P 数 =", runtime.GOMAXPROCS(0))
	const n = 600
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
		}()
	}
	fmt.Println("阻塞中的 G 数（含 main）=", runtime.NumGoroutine())
	close(start) // 600 个 G 同时变为可运行
	wg.Wait()
	fmt.Println("全部结束后 G 数 =", runtime.NumGoroutine())
}
```

<OutputBox title="第一段程序的输出示例（默认 P 数因机器而异）" output="默认 P 数 = 8\n调整后 P 数 = 2\n完成计数 = 8\n收尾后 G 数 = 1" note="把 GOMAXPROCS 调成 2 之后，8 个 G 仍全部完成：并行度变的是同时执行的数量，不是 G 的创建数量。收尾时只剩 main 一个 G。" />

<LineByLine title="findRunnable 的核心取 G 链路" code="gp := runqget(pp)\ngp = globrunqget(pp, 0)\ngp = runqsteal(pp, p2, true)\nexecute(gp, inheritTime)" :notes='["先查当前 P 的本地队列 runqget，命中 runnext 时几乎零成本", "本地为空则取全局队列 globrunqget，防止某些 G 长期得不到执行", "全局也空就随机挑另一个 P 用 runqsteal 偷一半，保持所有 P 忙碌", "拿到 G 后 execute 切换到它的栈运行，并继承剩余时间片"]' />

## 3. 深入一步
findRunnable 的真实顺序比上面四行更长：它先照顾 trace reader 与 GC worker，再查本地队列、全局队列、netpoll 就绪的 G，最后才做工作窃取。这个顺序不是随意排的，而是把「无锁、局部性最好」的路径放在最前，把「要加锁、要跨 P」的路径放在最后。sysmon 则完全不参与取 G，它每隔一段时间检查：P 是否卡在系统调用里太久（触发 handoffp 把 P 交出去）、是否有 G 运行超过 `forcePreemptNS`（设置抢占标志，由 `asyncPreempt` 在安全点插入）、以及是否需要触发 GC。

<Term word="异步抢占" text="Go 1.14 引入的机制：sysmon 给运行超时的 G 打上抢占标志，运行时在合适的安全点插入 asyncPreempt 调用，使纯计算循环也能被换下，无需代码里主动让出。" />

| 字段或常量 | 位置（Go 1.23.8） | 作用 | 观察线索 |
| --- | --- | --- | --- |
| `runq [256]guintptr` | `runtime2.go` 的 `p` | P 的无锁本地队列，容量固定 256 | 一次就绪超过 256 即溢出全局 |
| `runnext guintptr` | `runtime2.go` 的 `p` | 刚被唤醒、优先继承时间片的 G | 通信唤醒的延迟最低 |
| `runq gQueue` | `runtime2.go` 的 `schedt` | 全局队列，需加锁 | schedtrace 里的 `runqueue` 值 |
| `sysmontick` | `runtime2.go` 的 `p` | 上次记录 schedtick 与 syscalltick | sysmon 判定 P 是否卡在系统调用 |
| `forcePreemptNS` | `proc.go` 10ms | 单个 G 的时间片上限 | 超时后发出抢占请求 |

<Callout type="warn" title="GOMAXPROCS 的默认值会随版本变化" text="Go 1.23 默认取 NumCPU；后续版本在容器环境里会参考 CPU limit 做自动调整，可用 GODEBUG 相关开关关闭。不要凭记忆下结论，一律用 runtime.GOMAXPROCS(0) 实测。另外，把值调到远大于物理核只会增加切换开销。" />

## 4. 工程实践
线上出现「CPU 没跑满但延迟高」时，先看是不是 P 闲置或 G 排队。`GODEBUG=schedtrace=1000,scheddetail=1` 每秒打印一次各 P 的 `runq` 长度、全局 `runqueue` 与 `nmspinning`；若要精确到微秒级的调度延迟，再用 `runtime/trace` 采集后交给 `go tool trace` 看 Proc 视图。这两者都是只读观测，采样窗口控制在几十秒内即可。

```bash
GODEBUG=schedtrace=1000,scheddetail=1 ./myserver 2> sched.log
```

<Compare title="CPU 密集循环怎么写才不拖累调度" :panels='[{"label":"写法 A：手工让出","code":"for _, it := range items {\n    work(it)\n    runtime.Gosched()\n}","note":"每次迭代都主动让出，语义直白但让出过于频繁，吞吐下降，且是旧版本遗留写法"},{"label":"写法 B：依赖异步抢占","code":"for _, it := range items {\n    work(it)\n    if ctx.Err() != nil {\n        return\n    }\n}","note":"循环里有函数调用或阻塞点即可被安全抢占，配合 context 还能及时取消，用 schedtrace 验证无长尾"}]' />

## 动手练一练
<Exercise title="用 schedtrace 观察队列溢出" task="把第三段程序的 n 改成 600 配合 schedtrace 运行，观察全局 runqueue 峰值；再改成 100 对比。" :hints='["用 GODEBUG=schedtrace=100 缩短采样间隔", "关注 runqueue 字段"]' solution="n := 600\nstart := make(chan struct{})\nwg.Add(n)\nfor range items {\n    go func() {\n        defer wg.Done()\n        waitOn(start)\n    }()\n}\nclose(start)\nwg.Wait()" explain="n 为 600 时大量 G 同时就绪，超过本地队列容量 256，溢出到全局队列；n 为 100 时几乎无溢出。完整代码见第三段。" />

## 随堂检测
<Quiz no="95-1" q="findRunnable 在常规路径上的取 G 优先顺序是？" :options='["全局、本地、窃取", "本地、全局、窃取", "窃取、本地、全局", "netpoll、本地、全局"]' answer="1" explain="先无锁查本地 runqget，再取全局 globrunqget，最后跨 P 做 runqsteal，越靠前代价越低。" />
<Predict no="95-2" q="GOMAXPROCS(1) 下，一个无函数调用的纯计算紧循环，会让 main 永远拿不到 CPU 吗？" :options='["会，必须手工 Gosched", "不会，sysmon 约 10ms 后异步抢占", "会，但只在 Go 1.13 及以前"]' answer="1" output="100ms 内 main 仍被调度：异步抢占生效" explain="Go 1.14 起 sysmon 按 forcePreemptNS 打抢占标志，asyncPreempt 把超时 G 换下。" />

## 一页纸速记
<KnowledgeCard :points='["schedule 从 top 标签重新挑 G", "取 G 顺序：本地、全局、窃取", "p.runq 容量 256，超出溢出全局队列"]' :pitfalls='["GOMAXPROCS 远大于核数：只增切换开销", "死记默认 P 数：版本规则不同", "以为 sysmon 会帮忙取 G"]' summary="调度器只回答两件事：怎么最快拿到下一个 G，怎么不让任何 G 饿死。" />

## 小结与下一讲
1. schedule 是死循环，findRunnable 按「本地、全局、netpoll、窃取」的代价从低到高找 G。
2. `runq [256]guintptr` 是 P 的无锁本地队列，超出的 G 进入加锁的全局队列。
3. sysmon 不参与取 G，它负责 10ms 时间片的异步抢占、系统调用 handoff 与 GC 触发，可用 schedtrace 和 trace 观测。

[下一讲：GC 源码与真实调优案例](/advanced/runtime-gc)
