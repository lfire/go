---
title: 第 49 讲 · 数据竞争、内存模型与 race detector
description: 用 happens-before 判定数据竞争，给出可复现竞态与修复对照，讲清 -race 的原理、开销、误报与漏报
---

<StageHeader stage="L4 · 并发编程" level="★★★★☆" time="45 分钟" goal="能发现并修复隐蔽的并发 bug" prereq="第 48 讲 · 并发模式：pipeline / fan-out / worker pool" />

# 第 49 讲 · 数据竞争、内存模型与 race detector

> 「能跑」和「正确」之间隔着数据竞争：它不一定崩溃、不一定复现，可能悄悄少算一次、偶发返回旧值。本讲先用 happens-before 给出判定，再给可复现示例与三种修复，最后拆开 -race 的原理、开销与盲区。

## 这一讲你会得到什么
<CheckList id="lesson-49" title="本讲目标" :items='["用 happens-before 判定两个访问是否构成竞争","复现 counter++ 竞态并读懂 -race 报告","用 Mutex、atomic、channel 三种方式修复","说清 -race 的原理、开销、误报与漏报"]' />

## 1. 先建立直觉
把共享变量想成公共白板，happens-before 是「排队规则」：两操作（至少一写）有明确先后就安全，排不成先后即数据竞争，结果未定义。Go 内存模型只保证「有同步就有顺序」。编译器重排、CPU 让多个 G 并行，于是 `counter++` 的「读、加一、写回」三步可能被两个 G 同时读到旧值，一次更新就蒸发。

<ConceptDiagram type="gmp" title="多个 G 并行读写同一块内存" caption="多个 goroutine 被调度到不同 M 真正并行，访问之间又无同步排序时即发生数据竞争" />

## 2. 语法与规则
先看能复现的错误示例。1000 个 goroutine 对同一 int 自增，理论得 1000，实际常偏小；`go run -race` 还会打印冲突调用栈。

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var counter int
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter++ // 数据竞争：读改写不是原子
		}()
	}
	wg.Wait()
	fmt.Println("期望 1000，实际：", counter) // 常小于 1000
}
```

<OutputBox title="运行结果" output="期望 1000，实际： 973（每次不同）；-race 会打印 WARNING: DATA RACE 及冲突调用栈" note="counter++ 拆成读改写三步，交错会互相覆盖；-race 能指出冲突行号与调用栈" />

第一种修复是用互斥锁把读改写圈成临界区。锁不仅互斥，还在 Unlock 与后续 Lock 间建立 happens-before，让前一次写入对后一次可见。

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var (
		mu      sync.Mutex
		counter int
		wg      sync.WaitGroup
	)
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			counter++ // 临界区独占访问
			mu.Unlock()
		}()
	}
	wg.Wait()
	fmt.Println("期望 1000，实际：", counter) // 稳定 1000
}
```

临界区三行值得逐行看。

<LineByLine code="mu.Lock()\ncounter = counter + 1\nmu.Unlock()" :notes='["Lock 与上次 Unlock 形成同步，保证看到之前的写入","读改写被独占执行，不会被其它 goroutine 插进来覆盖","Unlock 释放锁，并把本次写入发布给下一个持锁者"]' title="加锁后的临界区" />

## 3. 深入一步
另两种修复：原子操作最轻量，channel 把「谁有权改」变成消息传递。下面一个程序同时给出，结果稳定。

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// 修复二：原子操作，适合单个数
func byAtomic() int64 {
	var n atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.Add(1)
		}()
	}
	wg.Wait()
	return n.Load()
}

// 修复三：channel 传递所有权，同一时刻只有一个改数据
func byChannel() int {
	ch := make(chan int, 1)
	ch <- 0 // 把所有权放进通道
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := <-ch   // 取到所有权
			ch <- v + 1 // 改完放回
		}()
	}
	wg.Wait()
	return <-ch
}

func main() {
	fmt.Println("atomic 修复：", byAtomic())
	fmt.Println("channel 修复：", byChannel())
}
```

能建立 happens-before 的同步有：go 语句、channel 收发与 close、Mutex 的 Unlock 与 Lock、WaitGroup 的 Done 与 Wait、Once 与 atomic。

三种修复策略的取舍：

| 策略 | 适合场景 | 优点 | 局限 |
| --- | --- | --- | --- |
| Mutex | 复合逻辑、多字段不变式 | 语义最强最好读 | 高竞争有阻塞开销 |
| atomic | 单计数、标志、指针 | 无锁开销最低 | 只护一个变量 |
| channel | 传所有权、流水线 | 契合 CSP 少共享 | 改造成本高，注意关闭顺序 |

<Term word="happens-before" text="Go 内存模型的偏序：若 A happens-before B，则 A 的写入对 B 可见；访问同一变量且至少一写、又无此排序，即数据竞争" />

<Callout type="warn" title="-race 的原理与局限" text="race detector 基于 ThreadSanitizer：编译期给读写插桩，运行时用影子内存与向量时钟，发现未同步的冲突访问（至少一写）就报告。代价是内存约 5 到 10 倍、CPU 慢 2 到 20 倍，需 CGO，只用于测试与 CI。它只检测本次执行到的交错：没跑到的分支、cgo 或汇编访问会漏报；碰巧正确的访问也会被报出，易被误当误报。它抓数据竞争，不抓逻辑竞态。" />

<Callout type="tip" title="工程化建议" text="把 go test -race ./... 接进 CI。读报告抓 Read at 与 Previous write at 两段栈，定位哪两个 goroutine 缺同步。修复优先级：先用 channel 消除共享，再按不变式复杂度选 Mutex 或 atomic。" />

## 4. 工程实践
线上数据竞争常伪装成「偶发计数不准」。怀疑时先用 -race 复跑，而不是加日志猜；race 构建改变时序，有的 bug 反而不出现，需配合压力多次运行。修复别只求「让它不报」，要把锁粒度、channel 所有权理清，否则可能把竞争从变量 A 挪到 B。团队规范应写明共享变量由谁保护。

<Compare :panels='[{"label":"写法 A：有竞争","code":"counter++ // 多个 goroutine 并发","note":"读改写可交错，更新丢失，-race 会报 DATA RACE"},{"label":"写法 B：加锁修复","code":"mu.Lock(); counter = counter + 1; mu.Unlock()","note":"临界区独占，Unlock 与后续 Lock 建立 happens-before，结果确定"}]' title="竞态与修复对照" />

## 动手练一练
<Exercise title="修复一段计数竞态" task="100 个 goroutine 各给 counter 加一，请用 Mutex 修复竞争并打印结果。" :hints='["声明 sync.Mutex 并前后 Lock、Unlock","Add 在 go 之前，Done 用 defer","wg.Wait() 后再读结果"]' solution="var mu sync.Mutex\nvar counter int\nvar wg sync.WaitGroup\nfor i := 0; i != 100; i++ {\n    wg.Add(1)\n    go func() {\n        defer wg.Done()\n        mu.Lock()\n        counter = counter + 1\n        mu.Unlock()\n    }()\n}\nwg.Wait()\nfmt.Println(counter)" explain="锁把读改写圈成不可分割的临界区，并在解锁与加锁间建立 happens-before，100 次更新一次不丢，输出 100。也可改用 atomic 或 channel。" />

## 随堂检测
<Quiz no="49-1" q="关于 -race，下面说法正确的是？" :options='["靠静态分析找出所有竞态，无漏报","应长期开在生产做监控","编译期插桩、运行时用向量时钟检测未同步冲突访问，开销大只用于测试","能查出所有逻辑竞态，如检查后使用的时序 bug"]' answer="2" explain="-race 动态检测，开销数倍且只覆盖本次执行路径；它抓数据竞争而非逻辑竞态，适合测试与 CI。" />
<Predict no="49-2" q="给 counter++ 加上 Mutex 后，1000 个 goroutine 各加一次，最终输出是？" :options='["总是 1000","通常小于 1000","随机崩溃","编译失败"]' answer="0" output="期望 1000，实际： 1000" explain="互斥锁让读改写独占执行，Unlock 与后续 Lock 建立 happens-before，更新不被覆盖。" />

## 一页纸速记
<KnowledgeCard :points='["判据：两访问同一变量至少一写且无 happens-before","go、channel、Mutex、WaitGroup、Once、atomic 建同步","counter++ 是读改写三步，交错会丢更新","-race 插桩加向量时钟检测，开销数倍","三修复：锁、原子、channel"]' :pitfalls='["把 -race 长开生产或以为能查所有竞态","只求报告消失，没理清变量由谁保护"]' summary="先用 happens-before 判竞争，再用锁、原子或 channel 建同步，用 -race 验证" />

## 小结与下一讲

- 判据是「两访问同一变量、至少一写、无 happens-before」，结果未定义。
- 修复：Mutex 护不变式、atomic 护单变量、channel 传所有权，都靠建同步。
- -race 开销大、只覆盖本次路径、不查逻辑竞态，应进 CI。

并发编程到此收束，接下来把代码变成可维护、可测试、可发布、可诊断的工程。

[下一讲：阶段五 · 工程化与质量](/engineering/)
