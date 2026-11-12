---
title: 第 44 讲 · select、超时与多路复用
description: 用 select 同时监听多个 channel，掌握随机选择、default 非阻塞、超时控制与 done channel
---

<StageHeader stage="L4 · 并发编程" level="★★★☆☆" time="40 分钟" goal="写出不会卡死的并发控制" prereq="第 43 讲 · channel 底层 hchan 与收发队列" />

# 第 44 讲 · select、超时与多路复用

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

## 4. 工程实践
真实服务里 select 几乎总是「数据 + 超时 + 停止」三件套：HTTP handler 等下游结果、等 deadline、等客户端断开；后台 worker 抢任务、等心跳、等优雅退出。经验是任何可能阻塞的等待都要配超时或停止出口，否则一个卡住的下游就能拖垮整组 goroutine；定时器复用而非每次新建，done 用 close 广播。

<Compare :panels='[{"label":"写法 A：循环里直接用","code":"time.After(d)","note":"每次新建 timer，命中快时旧 timer 堆积到 d 到期才回收"},{"label":"写法 B：显式持有复用","code":"timer := time.NewTimer(d); defer timer.Stop(); timer.Reset(d)","note":"只建一次，Stop 主动释放，Reset 复用，是长循环正确写法"}]' title="超时写法的取舍" />

## 动手练一练
<Exercise title="让定时任务干净停下" task="用 time.NewTicker 每 10ms 打印一次，累计 3 次后退出并释放定时器，要求 defer 释放、用 for range 接收。" :hints='["ticker.C 可用 for range 接收","defer ticker.Stop() 不能漏","用计数变量达到 3 就 return"]' solution="计数器 := 0\nticker := time.NewTicker(10 * time.Millisecond)\ndefer ticker.Stop()\nfor range ticker.C {\n    计数器 = 计数器 + 1\n    fmt.Println(计数器)\n    if 计数器 == 3 {\n        return\n    }\n}" explain="Ticker.C 是只读 channel，for range 阻塞式接收；计数到 3 时 return，defer 保证 Stop 释放 runtime timer。这正是替代循环里不断 time.After 的思路。" />

## 随堂检测
<Quiz no="44-1" q="select 有多个 case 同时就绪时，Go 会怎么做？" :options='["按书写顺序选第一个","伪随机挑一个就绪 case","选缓冲最大的那个","编译报错"]' answer="1" explain="运行时把就绪 case 收集后伪随机选一个，保证各路长期公平，避免固定顺序让某些通道饿死。" />
<Predict no="44-2" q="两个 channel 都为空、select 带 default，结果是？" :options='["阻塞直到有数据","立刻执行 default","因全阻塞而 panic","编译失败"]' answer="1" output="立刻执行 default，select 不阻塞" explain="没有 case 就绪时 default 立即执行，这是实现非阻塞收发、避免空等的关键。" />

## 一页纸速记
<KnowledgeCard :points='["多个 case 就绪时伪随机选，不保证顺序","default 让 select 永不阻塞","超时用 time.NewTimer 加 defer Stop，别反复 time.After","close(done) 广播唤醒所有接收者","优先级靠带 default 的非阻塞探测"]' :pitfalls='["循环里 time.After 堆积 timer 泄漏","阻塞等待没配超时或停止出口","误以为 select 按书写顺序执行"]' summary="select 把多路通信、超时与停止并成一次等待：谁先就绪走谁，没人就绪走 default，到期走 timer" />

## 小结与下一讲

- select 多路复用：就绪 case 伪随机选一个，default 提供非阻塞出口。
- 超时用显式 timer 并主动 Stop，循环滥用 time.After 会累积 timer；close(done) 广播叫停。
- 优先级靠双层 select，任何阻塞等待都要留超时或停止出口。

[下一讲：sync：Mutex / RWMutex / WaitGroup / Once / Cond](/concurrency/sync-primitives)
