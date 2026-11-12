---
title: 第 99 讲 · Go 内存模型与 happens-before 精解
description: 用严格术语解释 sequenced-before、synchronized-before、happens-before 与 DRF-SC，给并发正确性一个可依据的判据
---

<StageHeader stage="L9 · 资深进阶与源码" level="★★★★★" time="50 分钟" goal="对并发正确性给出严格依据" prereq="第 49 讲 · 数据竞争、内存模型与 race detector" />

# 第 99 讲 · Go 内存模型与 happens-before 精解

> 第 49 讲用 race detector 抓过竞态，但「为什么这样写算错」「凭什么加了 channel 就一定对」需要更严格的回答。Go 官方内存模型给出的答案是 happens-before 关系：它不描述 CPU 怎么乱序，而是规定一个读必须能看到哪个写。本讲用规范术语拆解三个关系、数据竞争的形式化定义与 DRF-SC 保证，再用可运行程序和 `-race` 把每条结论坐实。规范文本见 GOROOT 的 `doc/go_mem.html`，源码以 Go 1.23.8 为准。

## 这一讲你会得到什么
<CheckList id="L9-99" title="本讲目标" :items='["区分 sequenced-before、synchronized-before 与 happens-before", "用规范定义判断一段代码是否存在数据竞争", "说出 channel/Mutex/Once/atomic 各自建立的同步保证"]' />

## 1. 先建立直觉
多个 G 分派到多个 P 上并发执行，编译器重排指令、CPU 乱序与缓存读写。只靠「我明明先写了」判断结果一定会翻车。内存模型不谈物理细节，而是给一个偏序：规定哪些操作之间已有明确的先后保证，让「这次读能看到哪次写」成为可推导的结论。
<ConceptDiagram type="gmp" title="为什么需要内存模型" caption="多个 G 并发执行，内存模型规定它们能观察到彼此写入的哪些值" />

## 2. 语法与规则
规范把执行建模为一组 goroutine 执行加上映射 W（每个读从哪个写取值），再定义三种关系：

- **sequenced before**：单个 goroutine 内，由 Go 语言规范控制流与求值顺序规定的偏序。
- **synchronized before**：由同步操作建立的跨 goroutine 顺序，如 channel 收发、Mutex 的 Unlock/Lock、Once、atomic。
- **happens before**：前两者并集的**传递闭包**（`doc/go_mem.html` 153 行）。规范原文把它定义为 sequenced before 与 synchronized before 之并的传递闭包。

无数据竞争的程序满足 **DRF-SC**：表现得像所有 goroutine 被复用在单个处理器上顺序一致地执行（同文件 59 行）。数据竞争的形式化定义是：对同一内存位置的一次写，与另一次读或写并发发生，且这些访问不全是 `sync/atomic` 提供的原子访问，同时两者不被 happens before 排序。

三段程序分别展示正确的同步边、错误样例、以及三类原语的保证。

```go
package main

import (
	"fmt"
	"runtime"
	"sync/atomic"
)

// 观察一：用 atomic 的 Store/Load 建立 synchronized-before，
// (1) 写 data 与 (2) Store synchronizes before (3) Load 与 (4) 读 data，
// 因此 (4) 必然看到 42。
var data int
var ready int32

func main() {
	go func() {
		data = 42
		atomic.StoreInt32(&ready, 1)
	}()
	for atomic.LoadInt32(&ready) == 0 {
		runtime.Gosched()
	}
	fmt.Println("data =", data)
}
```

```go
package main

import (
	"fmt"
	"runtime"
)

// 观察二：错误样例。普通 bool 的写与读之间没有 synchronized-before，
// 构成数据竞争；用 go run -race 会报告，不能用「跑几次没出错」当依据。
var data int
var ready bool

func main() {
	go func() {
		data = 42
		ready = true
	}()
	for i := 0; i < 1_000_000 && !ready; i++ {
		runtime.Gosched()
	}
	fmt.Println("data =", data)
}
```

```go
package main

import (
	"fmt"
	"sync"
)

// 观察三：channel、Mutex、Once 分别建立 synchronizes-before 边。
func main() {
	ch := make(chan int)
	x := 0
	go func() {
		x = 42
		ch <- 1
	}()
	<-ch
	fmt.Println("channel 保证 x =", x)

	var mu sync.Mutex
	y := 0
	mu.Lock()
	y = 7
	mu.Unlock()
	mu.Lock()
	fmt.Println("Mutex 保证 y =", y)
	mu.Unlock()

	var once sync.Once
	z := 0
	once.Do(func() { z = 9 })
	fmt.Println("Once 保证 z =", z)
}
```

<OutputBox title="第三段程序的确定性输出" output="channel 保证 x = 42\nMutex 保证 y = 7\nOnce 保证 z = 9" note="三条边分别由发送/接收、Unlock/Lock、Do 返回建立，因此每个读都必然看到之前的写。" />

<LineByLine title="happens-before 的定义链" code="sequenced before\nsynchronized before\nhappens before = 传递闭包\nDRF-SC 保证" :notes='["单个 G 内按语言规范的控制流与求值顺序形成的偏序", "由 channel、Mutex、Once、atomic 等同步操作建立的跨 G 顺序", "取前两者之并再取传递闭包，是判断可见性的核心关系", "无数据竞争的程序表现为顺序一致，像所有 G 复用在单处理器上"]' />

## 3. 深入一步
规范对普通读 r 要求 W(r) 必须是「可见」的写 w：w happens before r，且不存在另一个写 w' 满足 w happens before w' happens before r。换句话说，读看到的不是「最后写完的那个值」，而是 happens-before 偏序里紧邻它的那个写。一旦两个冲突访问没有被这条偏序排开，就是数据竞争，DRF-SC 保证随之失效。

同步保证在标准库注释里都写得很直白：`sync/mutex.go` 29 行说第 n 次 Unlock synchronizes before 第 m 次 Lock（n < m），且失败的 TryLock 不建立任何关系；`sync/once.go` 16 行说 f 的返回 synchronizes before 任意 Do 的返回；`sync/atomic/doc.go` 42 行说原子操作表现为某个顺序一致的执行。硬件层面，编译器与 CPU 可以自由重排，只要不破坏这些 synchronizes-before 边；这正是「语言内存序」与「硬件内存序」的分工。

<Term word="DRF-SC" text="Data-Race-Free Sequential Consistency：不含数据竞争的程序，其所有执行都等价于把各 goroutine 交错复用在单一处理器上的顺序一致执行。" />

| 同步原语 | 建立的 synchronized-before | 源码依据（Go 1.23.8） |
| --- | --- | --- |
| channel 发送 → 接收 | 发送 synchronizes before 对应接收完成 | `runtime/chan.go` |
| Mutex.Unlock → Lock | 第 n 次 Unlock synchronizes before 第 m 次 Lock（n < m） | `sync/mutex.go:29` |
| Once.Do 返回 | f 的返回 synchronizes before 任意 Do 的返回 | `sync/once.go:16` |
| atomic 操作 | 全体原子操作等价于某个顺序一致执行 | `sync/atomic/doc.go:42` |
| WaitGroup.Done → Wait | Done synchronizes before Wait 的返回 | `sync/waitgroup.go` |

<Callout type="warn" title="跑对了不等于写对了" text="有竞争的程序不受 DRF-SC 保护，可能读到旧值或被优化成死循环。`-race` 只发现本次执行碰到的竞争，没报不代表没有；正确性必须靠 happens-before 推导。" />

## 4. 工程实践
代码评审时把「共享变量 + 是否有同步边」当成固定检查项：先列出所有跨 goroutine 的读写，再逐个追问两者之间有没有 channel、Mutex、Once、WaitGroup 或 atomic 建立的边；没有就补上，而不是加 `time.Sleep`。CI 里固定跑 `go test -race`，并对 `go_mem.html` 里「实现可以对数据竞争直接报告并终止」这一条保持敬畏。

```bash
go run -race main.go
go test -race ./...
```

<Compare title="发布信号的两种写法" :panels='[{"label":"写法 A：普通 bool","code":"go func() {\n    data = 42\n    ready = true\n}()\nfor !ready {\n    spin()\n}\nprintln(data)","note":"写与读之间无 synchronized-before，是数据竞争，结果不可推导"},{"label":"写法 B：atomic 或 channel","code":"go func() {\n    data = 42\n    atomic.StoreInt32(ptr, 1)\n}()\nfor atomic.LoadInt32(ptr) == 0 {\n    spin()\n}\nprintln(data)","note":"Store/Load 建立 synchronized-before，读必然看到 42，可用 -race 验证"}]' />

## 动手练一练
<Exercise title="用 -race 抓一次数据竞争" task="把第二段程序用 go run -race 运行，记录报告里的两个访问位置；再改成第一段写法，确认报告消失。" :hints='["报告会同时给出写与读的栈", "关注 WARNING: DATA RACE 与 Found 1 data race"]' solution="var data int\nvar ready int32\ngo writer()\nfor atomic.LoadInt32(ptr) == 0 {\n    runtime.Gosched()\n}\nprintln(data)" explain="普通 bool 版本被报告竞争；换成 atomic 后同步边成立、报告消失。完整代码见第一段。" />

## 随堂检测
<Quiz no="99-1" q="按 Go 官方内存模型，happens-before 是如何定义的？" :options='["所有 goroutine 的真实执行时间顺序", "CPU 保证的指令先后", "sequenced-before 与 synchronized-before 之并的传递闭包", "GC 停顿点之间的顺序"]' answer="2" explain="doc/go_mem.html 明确定义 happens before 为 sequenced before 与 synchronized before 并集的传递闭包。" />
<Predict no="99-2" q="第二段程序（普通 bool 标志）在 -race 下运行，结果最准确的描述是？" :options='["必定打印 42", "必定打印 0", "报告数据竞争，结果不可依据"]' answer="2" output="报告数据竞争，结果不可依据" explain="写 data 与读 data 之间没有 synchronized-before 边，构成数据竞争；DRF-SC 不适用，不能对输出作任何保证。" />

## 一页纸速记
<KnowledgeCard :points='["happens-before 是两个关系并集的传递闭包", "DRF-SC：无竞争程序表现为顺序一致", "同步边来自 channel、Mutex、Once、WaitGroup、atomic"]' :pitfalls='["用普通变量做 ready 标志", "靠 Sleep 代替同步", "把 -race 没报当成没竞争"]' summary="并发正确性靠 happens-before 推导，不靠运行次数或物理先后。" />

## 小结与下一讲
1. sequenced-before 管单 G 内顺序，synchronized-before 管跨 G 同步，两者并集的传递闭包即 happens-before。
2. 数据竞争是「冲突访问不被 happens-before 排序」；无竞争程序享有 DRF-SC 保证。
3. channel、Mutex、Once、WaitGroup、atomic 各自建立明确的同步边，CI 固定跑 `-race`。

[下一讲：极致性能优化：从 pprof 到 30w QPS](/advanced/perf-case)
