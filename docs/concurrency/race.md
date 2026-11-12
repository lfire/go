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

### 再深一层：happens-before 的形式化、-race 的向量时钟与盲区

官方内存模型文档（本机 `$GOROOT/doc/go_mem.html`）的定义是：数据竞争为「对同一内存位置的一次写与另一次读或写并发发生，且这些访问不全是对 `sync/atomic` 的原子访问」；`synchronized before` 是同步操作上的偏序——「若同步型读 r 观察到同步型写 w，则 w synchronized before r」；happens-before 是 `sequenced before`（单 goroutine 内的程序顺序）与 `synchronized before` 并集的传递闭包。文档还有一句最关键的承诺：无数据竞争的程序表现得像所有 goroutine 被复用到单处理器上，即 DRF-SC（data-race-free 程序顺序一致）。反之存在竞争时结果不受任何顺序约束：编译器可重排、CPU 可乱序、可见性无保证，`counter++` 少算只是最温和的后果。这也说明「这次跑对了」不能证明正确。

`-race` 把这套判据变成了可执行的检测。开启后编译器在每次内存读写前后插入 `runtime.racewrite`/`raceread` 调用，运行时用影子内存为每个 8 字节应用内存维护若干向量时钟槽，每个 G 带自己的时钟；当一次访问发现同一地址上存在「至少一写、且向量时钟互不相序」的先前访问，就打印 `WARNING: DATA RACE`。报告给两段冲突栈（`Write at` / `Previous read at`）及创建点，读法是顺着栈找缺同步的访问。它是纯动态检测：只覆盖本次执行到的交错，没跑到的分支、cgo 与汇编访问会漏报；`GORACE="halt_on_error=1"` 可命中即停，`history_size` 调大保留更多历史。至于「误报」：严格说它不产生误报，报出的都是运行时观测到的真实竞争；所谓误报多半是「这次恰好跑对但代码仍有竞争」。真正的盲区是逻辑竞态：TOCTOU、死锁、goroutine 泄漏、跨服务时序，`-race` 一概不查。

本讲给的 `publishBad` 是教科书级的「未同步发布」：写方 `data = 42; ready = true`，读方自旋等 `ready` 再读 `data`。`go` 语句只建立「main 在 go 之前的动作」到「goroutine 起点」的 happens-before，而 main 的自旋读与 goroutine 的 `ready = true` 之间没有任何同步边，向量时钟判为并发：可能永远看不到 true，也可能看到 true 却读到旧 `data`。`go test -race -run=TestPublish -v ./...` 让 channel 发布版本全绿；`SHOW_RACE=1 go test -race -run=TestPublishBadRace ./...` 则精确打印上述两段栈。生产里怀疑偶发计数不准，先把 `go test -race ./...` 接进 CI、配合 `go test -count=100` 放大交错，读报告定位变量后再决定用 channel 消除共享，还是按不变式复杂度选 Mutex 或 atomic；别只求报告消失，否则竞争常被从变量 A 挪到 B。

```go
package main

import (
	"os"
	"sync"
	"testing"
)

// 错误对照：用普通 bool 发布数据，写方与读方无 happens-before
func publishBad() int {
	var data int
	var ready bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		data = 42
		ready = true // 与读方的读无同步
	}()
	for i := 0; i < 1000 && !ready; i++ {
	}
	wg.Wait()
	return data
}

// 正确：用 channel 发布，收发生成 happens-before
func publishOK() int {
	ready := make(chan int, 1)
	go func() {
		ready <- 42
	}()
	return <-ready
}

func TestPublish(t *testing.T) {
	for i := 0; i < 100; i++ {
		if got := publishOK(); got != 42 {
			t.Fatalf("期望 42，实际 %d", got)
		}
	}
}

// 反面实验：设 SHOW_RACE=1 并配合 -race，可看到 bool 发布的 DATA RACE
func TestPublishBadRace(t *testing.T) {
	if os.Getenv("SHOW_RACE") == "" {
		t.Skip("反面实验：设 SHOW_RACE=1 并加 -race 运行")
	}
	_ = publishBad()
}
```

```bash
go test -race -run=TestPublish -v ./...                    # 正确发布：全绿
SHOW_RACE=1 go test -race -run=TestPublishBadRace ./...      # 反面实验：打印两段冲突栈
GORACE="halt_on_error=1" go test -race ./...                # 命中首个竞态即停，便于读栈
```

把各检测手段的「依据、能抓、不能抓」分开列：

| 检测手段 | 依据 | 能抓 | 不能抓与边界 |
| --- | --- | --- | --- |
| `go test -race`（ThreadSanitizer） | 插桩加影子内存加向量时钟 | 运行时观测到的、无 happens-before 的冲突读写 | 未执行分支、cgo 与汇编访问、逻辑竞态、死锁、goroutine 泄漏 |
| `go vet` | 静态分析 | copylocks、lostcancel、部分明显误用 | 绝大多数并发 bug，无运行时信息 |
| `go test -count=100` 压力 | 放大交错概率 | 偶发结果错、漏关 channel 的表现 | 不报原因，可能仍复现不了 |
| `runtime.NumGoroutine` 与 goleak | G 数量 | goroutine 泄漏 | 数据竞争、逻辑竞态 |
| `go tool pprof -mutex` 与 block profile | 争用与阻塞采样 | 锁热点、阻塞时长 | 正确性，只给性能线索 |
| 评审时明确「谁保护哪个变量」 | happens-before 设计 | 从源头消除共享 | 依赖规范落地执行 |

下面这张时序图把 `publishBad` 里两步写与两步读的交错、以及检测器为何判为并发画出来：

<SequenceDiagram title="未同步发布的竞态交错" caption="main 的自旋读与 writer 的 ready 写之间无同步边，向量时钟无序，检测器报 DATA RACE" :actors='["main","writer G","ready","data","检测器"]' :steps='[{"from":"main","to":"writer G","label":"go 启动","note":"HB 只到 goroutine 起点","kind":"call"},{"from":"writer G","to":"data","label":"写 42","note":"普通写非原子发布","kind":"call"},{"from":"writer G","to":"ready","label":"写 true","note":"与读无同步","kind":"call"},{"from":"main","to":"ready","label":"自旋读","note":"无锁检查","kind":"call"},{"from":"ready","to":"main","label":"读到 true","note":"两访问无排序","kind":"return"},{"from":"main","to":"data","label":"读 data","note":"可能仍是旧值","kind":"call"},{"from":"data","to":"main","label":"返回 0","note":"发布失败","kind":"return"},{"from":"检测器","to":"main","label":"DATA RACE","note":"向量时钟无序","kind":"async"}]' />

<Predict q="对 publishBad（普通 bool 发布）跑 go test -race，最可能看到什么？" :options='["正常输出 42，无任何提示","WARNING: DATA RACE，指出 ready 的读写无同步","编译报错","fatal error: all goroutines are asleep"]' answer="1" output="WARNING: DATA RACE 及两段冲突调用栈" explain="main 的自旋读与 writer 的 ready=true 之间没有 happens-before，向量时钟判定二者并发；go test -race 会打印 Write at 与 Previous read at 两段栈。改成 channel 发布后收发建立同步，报告消失。" />

<Quiz q="关于 -race 的『误报』与『漏报』，下面说法正确的是？" :options='["它会静态分析出所有潜在竞态，没有漏报","运行正确的访问被报出属于误报，应加抑制","它只在运行时观测到无排序的冲突访问才报，没跑到的分支与被碰巧掩盖的竞争会漏报","开启后应长期挂在进程里做线上监控"]' answer="2" explain="-race 基于本次执行的动态检测，报告的都是真实数据竞争而非误报；漏报来自未执行路径、cgo 与汇编访问以及有限的历史槽，逻辑竞态（检查后使用、死锁、泄漏）它本来就不查。开销数倍，只适合测试与 CI。" />

## 4. 工程实践
线上数据竞争常伪装成「偶发计数不准」。怀疑时先用 -race 复跑，而不是加日志猜；race 构建改变时序，有的 bug 反而不出现，需配合压力多次运行。修复别只求「让它不报」，要把锁粒度、channel 所有权理清，否则可能把竞争从变量 A 挪到 B。团队规范应写明共享变量由谁保护。

<Compare :panels='[{"label":"写法 A：有竞争","code":"counter++ // 多个 goroutine 并发","note":"读改写可交错，更新丢失，-race 会报 DATA RACE"},{"label":"写法 B：加锁修复","code":"mu.Lock(); counter = counter + 1; mu.Unlock()","note":"临界区独占，Unlock 与后续 Lock 建立 happens-before，结果确定"}]' title="竞态与修复对照" />

### 与 L10 实战的呼应

`-race` 与 happens-before 最终要在真实故障里兑现。第 116 讲 [秒杀与高并发库存](/projects/micro-seckill) 把「先查余量再扣减」的超卖根因归结为检查与扣减之间没有被同一个原子操作包住：两个并发读者基于同一旧值各自扣减，正是本讲 `counter++` 读改写被交错覆盖的业务版本，修法也是三选一——锁、原子，或把库存收进单 goroutine 用 channel 传所有权。第 110 讲 [分布式任务调度](/projects/concurrent-scheduler) 把「定时任务执行两次」列为资损事故，进程内用 `go test -race ./...` 守住共享状态、跨进程用幂等键与 fencing token 兜住至少一次语义。第 109 讲 [并发爬虫](/projects/concurrent-crawler) 的去重集合被多 worker 并发访问，若漏了同步就是典型的 concurrent map 竞态；把 `-race` 接进 CI，配合 `go test -count=100` 放大交错，才能在压测前就抓出这类「偶发计数不准」。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l4-concurrency/lesson-049-race`](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-049-race)。clone 仓库后进入 `examples/l4-concurrency`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

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
