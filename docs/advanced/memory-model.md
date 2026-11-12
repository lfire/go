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

### 源码精读与可复现验证（以 Go 1.21~1.23 为准，版本可能变化）

形式化定义的源头是 GOROOT 里的 `doc/go_mem.html`，它不是实现文档而是语言规范：把一次执行建模为一组 goroutine 的操作加上映射 `W`（每个读从哪个写取值），再定义 `sequenced before`、`synchronized before` 与二者并集的传递闭包 `happens before`，最后给出 DRF-SC 保证——不含数据竞争的程序，其行为等价于把各 goroutine 交错复用在单个处理器上的顺序一致执行。规范还规定普通读 `r` 只能看到「可见」的写 `w`：`w` happens before `r`，且不存在另一个写 `w'` 夹在两者之间。这就是「读看到的不是最后写完的值，而是偏序里紧邻它的那个写」的严格出处。

各同步原语的保证写在标准库注释里，读源码比记结论可靠。`src/sync/mutex.go` 开头注明第 n 次 `Unlock` synchronizes before 第 m 次 `Lock`（n 小于 m），且失败的 `TryLock` 不建立任何关系；`src/sync/once.go` 注明 `f` 的返回 synchronizes before 任意 `Do` 的返回；`src/sync/waitgroup.go` 注明 `Done` synchronizes before `Wait` 的返回；`src/sync/atomic/doc.go` 注明全体原子操作表现为某个顺序一致的执行；`src/runtime/chan.go` 里发送与对应接收完成构成一对。`-race` 的实现则是编译器插桩：带 `-race` 构建时，`cmd/compile` 在每次内存访问前后插入对 `runtime.raceread`、`runtime.racewrite` 一族函数的调用，由 `src/runtime/race/` 下的 ThreadSanitizer 运行时维护 happens-before 向量时钟，发现两个未被同步边排序的冲突访问就打印报告。一个可复现的观察实验：用 `go run -race` 分别跑第 2 段的错误 bool 版与第 1 段的 atomic 版，前者稳定报告竞争、后者无报告；再对错误版加 `go test -race -count=20` 重复采样，会发现报告出现的位置可能变化，但 `Found 1 data race(s)` 与退出码 66 始终一致。

适用边界要写清：以上规范文本与源码注释以 Go 1.21~1.23 为准，注释行号每版都会漂移，判断以术语与语义为准；`-race` 只发现本次执行实际碰到的冲突访问，未覆盖的分支、未触发的交错都查不到，所以「没报」绝不等于「没有」；DRF-SC 只保护无竞争的程序，一旦有竞争，语言不保证任何结果，编译器优化与 CPU 乱序都可能让程序表现得像死循环；硬件内存序（x86 的 TSO、ARM 的弱序）由编译器与运行时用屏障抽象掉，应用代码只应依赖语言级的 happens-before，不要按某款 CPU 的行为推理。

一条命令把第 2 段错误样例的竞态报告打出来：

```bash
go run -race main.go
GORACE="halt_on_error=1 history_size=5" go test -race -count=20 ./...
```

```text
==================
WARNING: DATA RACE
Write at 0x00c0000120a0 by goroutine 7:
  main.main.func1()
      /path/to/main.go:12 +0x2c

Previous read at 0x00c0000120a0 by main goroutine:
  main.main()
      /path/to/main.go:15 +0x5a

Goroutine 7 (running) created at:
  main.main()
      /path/to/main.go:11 +0x28
==================
Found 1 data race(s)
exit status 66
```

报告里每个字段都对应形式化定义：`WARNING: DATA RACE` 表示检测到两个冲突访问；`Write at 0x00c0000120a0 by goroutine 7` 给出写操作的内存地址、类型与发起者 id，`Previous read at` 同一地址 `by main goroutine` 给出与之冲突的另一次访问——两者地址相同即「同一内存位置」，而中间没有 channel、Mutex 或 atomic 的同步边，正是数据竞争定义里的「不被 happens before 排序」。两段栈帧的 `/path/to/main.go:12` 与 `:15` 是 `文件:行`，`+0x2c` 是函数内指令偏移，用来定位到具体语句；`Goroutine 7 ... created at` 追出该 goroutine 的创建点，便于补全调用链。`Found 1 data race(s)` 是本次执行发现的竞争数，`exit status 66` 是 race detector 让进程以 66 退出，CI 可以据此直接判定失败。`GORACE` 的 `halt_on_error=1` 让第一次竞争就停下，`history_size=5` 调大栈历史深度；`-count=20` 靠重复采样提高触发概率，但仍是采样手段，不能替代用 happens-before 推导正确性。

<SequenceDiagram title="一次正确同步建立的 happens-before 链" caption="atomic 的 Store 与 Load 之间形成同步边，使读必然看到之前的写" :actors='["G1","data","atomic","G2","-race"]' :steps='[{"from":"G1","to":"data","label":"写 data 为 42","note":"sequenced before","kind":"call"},{"from":"G1","to":"atomic","label":"Store 发布信号","note":"Store 建立同步边","kind":"call"},{"from":"atomic","to":"G2","label":"Load 观察到 1","note":"synchronized before","kind":"return"},{"from":"G2","to":"data","label":"读 data","note":"happens before 覆盖","kind":"call"},{"from":"data","to":"G2","label":"返回 42","note":"必然看到该写","kind":"return"},{"from":"G2","to":"-race","label":"检查本次访问","note":"地址与栈比对","kind":"call"},{"from":"-race","to":"G2","label":"不报告竞争","note":"无 WARNING","kind":"return"},{"from":"G2","to":"G2","label":"DRF-SC 成立","note":"顺序一致执行","kind":"self"}]' />

把第 3 步的 `return` 看成整条链的关键：Store 与 Load 配成一对同步边，才把 G1 里的 `sequenced before` 传递到 G2 的读上；少了它，前两步对 G2 就完全不可见。

| 观测手段 | 能发现什么 | 关键输出或开关 | 开销与局限 |
| --- | --- | --- | --- |
| `go run -race main.go` | 本次执行碰到的冲突访问 | `WARNING: DATA RACE` 与两条栈 | 只覆盖实际跑到的路径 |
| `go test -race ./...` | 测试路径里的竞争 | `Found N data race(s)` | 未测分支查不到 |
| `go test -race -count=20` | 提高交错触发概率 | 多次采样结果 | 仍是采样，不保证穷尽 |
| `GORACE halt_on_error=1` | 第一次竞争即停 | 配合 `history_size` 调栈深 | 仅本地诊断用 |
| 带 `-race` 构建 | 编译器插桩后的执行 | `runtime.raceread` 等调用 | 内存约 5 到 10 倍、耗时 2 到 20 倍 |

五项里前三项是日常 CI 的主力，后两项用于定位偶发问题；要记住 `-race` 是发现工具而非证明工具，真正的证明仍然来自 happens-before 推导。

<Callout type="tip" title="源码阅读建议" text="先通读 doc/go_mem.html 里 happens before 与 DRF-SC 两节拿到术语，再按 sync/mutex.go、once.go、waitgroup.go、atomic/doc.go 的顺序读各文件开头的同步保证注释；想深挖 -race 就搜 runtime.raceread 的插桩点，别从 ThreadSanitizer 的向量时钟实现读起。" />

### 常见误读与反例

内存模型的误读最危险，因为错的代码往往「跑几次都对」。下面四条用规范定义、`-race` 报告与一个最小反例就能推翻。

| 误读 | 为什么站不住（规范推导） | 正确写法或反例 |
| --- | --- | --- |
| 「`time.Sleep` 一会儿或加个 `runtime.Gosched()`，读就能看到之前的写」 | `sequenced before` 只在单个 G 内有效，`Sleep` 与 `Gosched` 都不建立 `synchronized before`；没有同步边就是数据竞争，DRF-SC 不适用。编译器甚至可能把 `for !ready {}` 优化成死循环 | 用 atomic 的 Store/Load、channel 或 Mutex 建立边；第 2 段错误样例与第 1 段正确样例就是同一逻辑的两种写法 |
| 「`-race` 没报就说明没有数据竞争，跑几次结果都对就说明写对了」 | `-race` 只发现本次执行实际碰到的冲突访问，未覆盖的分支、未触发的交错都查不到；有竞争的程序不受 DRF-SC 保护，语言不保证任何结果 | 靠 `happens-before` 推导正确性；用 `go test -race -count=20` 提高交错触发概率，但它仍是采样手段，不是证明 |
| 「原子变量只是更快，可见性和普通变量一样；x86 不会重排所以不用同步」 | `sync/atomic/doc.go` 规定全体原子操作等价于某个顺序一致执行，这才建立 synchronized-before；硬件内存序（x86 的 TSO、ARM 的弱序）由编译器与运行时用屏障抽象掉，应用只能依赖语言级 happens-before | 按第 3 节那张表选原语，别按某款 CPU 的行为推理；`go_mem.html` 的术语是唯一可移植的依据 |
| 「Mutex 的 TryLock 失败也建立了同步边」 | `sync/mutex.go` 开头明确写：第 n 次 Unlock synchronizes before 第 m 次 Lock（n 小于 m），而**失败的 TryLock 不建立任何关系** | 只有成功配对的 Lock/Unlock 才算边；用 TryLock 做「试探」后不能据此读共享数据 |

下面这个最小反例把第一条误读坐实：同样的发布逻辑，`Sleep` 与 `Gosched` 版本在 `-race` 下稳定报告竞争，atomic 版本无报告。

```go
package main

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"time"
)

// 反例：Sleep 与 Gosched 都不建立 synchronized-before。
// 单独用 go run -race 运行 wrong() 会报告数据竞争。
func wrong() {
	var data int
	var ready bool
	go func() {
		data = 42
		ready = true
	}()
	for i := 0; i < 1000 && !ready; i++ {
		runtime.Gosched()
	}
	time.Sleep(time.Millisecond)
	fmt.Println("wrong 读到 data =", data)
}

func right() {
	var data int
	var ready atomic.Int32
	go func() {
		data = 42
		ready.Store(1)
	}()
	for ready.Load() == 0 {
		runtime.Gosched()
	}
	fmt.Println("right 读到 data =", data)
}

func main() {
	wrong()
	right()
}
```

`wrong` 里 `data` 与 `ready` 的写读之间没有任何同步边，即使这次恰好打印 42 也不能作数；`right` 里 `atomic.Int32` 的 Store/Load 配成一对，才把 G1 内的 `sequenced before` 传递到 G2 的读上。用 `go run -race` 跑，`wrong` 部分会出现 `WARNING: DATA RACE` 与 `Found 1 data race(s)`，这正是第 3 节报告逐字段解释里的那条链。

<Quiz q="用 go test -race -count=20 跑一个含数据竞争的程序，全部 20 次都没报错，能得出什么结论？" :options='["程序没有数据竞争","本次采样的交错没碰到冲突访问，不能证明无竞争","-race 对该程序无效","加 Sleep 就能彻底修复"]' answer="1" explain="-race 只发现本次执行实际碰到的冲突访问，-count=20 只是提高触发概率，仍是采样；正确性只能靠 happens-before 推导。" />

一句话记边界：`-race` 报告能证明「这次碰到了一对未排序的冲突访问」，却不能证明「没碰到的分支里没有」；`go_mem.html` 的偏序推导才是证明，工具只是发现手段。

### 与 L10 源码级实战的呼应

- KV 贯穿线 ③ [WAL、持久化与并发控制](/projects/kv-3-async)：WAL 追加写与快照切换之间必须靠 Mutex 或 atomic 建立同步边，否则恢复线程可能看到「写了一半」的记录；本讲的 happens-before 推导是判断这段并发控制是否正确的唯一依据，`-race` 只能辅助发现。
- [实战 10 · 秒杀与高并发库存](/projects/micro-seckill)：预扣减与最终一致里，库存计数与订单状态的发布必须有一次同步边（channel 或 atomic），靠「先写库存再写订单」的书写顺序在并发下并不成立。
- [实战 08 · 分布式任务调度系统](/projects/concurrent-scheduler)：etcd 选主与租约故障转移时，主节点标志的读写跨多个 goroutine，`sync/atomic` 与 `WaitGroup.Done`/`Wait` 各自建立的边正好对上第 3 节那张同步原语表。

## 4. 工程实践
代码评审时把「共享变量 + 是否有同步边」当成固定检查项：先列出所有跨 goroutine 的读写，再逐个追问两者之间有没有 channel、Mutex、Once、WaitGroup 或 atomic 建立的边；没有就补上，而不是加 `time.Sleep`。CI 里固定跑 `go test -race`，并对 `go_mem.html` 里「实现可以对数据竞争直接报告并终止」这一条保持敬畏。

```bash
go run -race main.go
go test -race ./...
```

<Compare title="发布信号的两种写法" :panels='[{"label":"写法 A：普通 bool","code":"go func() {\n    data = 42\n    ready = true\n}()\nfor !ready {\n    spin()\n}\nprintln(data)","note":"写与读之间无 synchronized-before，是数据竞争，结果不可推导"},{"label":"写法 B：atomic 或 channel","code":"go func() {\n    data = 42\n    atomic.StoreInt32(ptr, 1)\n}()\nfor atomic.LoadInt32(ptr) == 0 {\n    spin()\n}\nprintln(data)","note":"Store/Load 建立 synchronized-before，读必然看到 42，可用 -race 验证"}]' />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l9-advanced/lesson-099-memory-model`](https://github.com/lfire/go/tree/main/examples/l9-advanced/lesson-099-memory-model)。clone 仓库后进入 `examples/l9-advanced`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

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
