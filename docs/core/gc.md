---
title: 第 33 讲 · 垃圾回收：三色标记、屏障与 GOGC
description: 从三色不变式推导写屏障的动机，讲清并发标记、两处 STW 与 GOGC / MemoryLimit 的取舍
---

<StageHeader stage="L3 · 核心机制" level="★★★★☆" time="50 分钟" goal="理解 GC 触发、停顿与调优" prereq="第 32 讲 · 内存分配器：mcache / mcentral / mheap" />

# 第 33 讲 · 垃圾回收：三色标记、屏障与 GOGC

> Go 的 GC 不用引用计数、也不做分代，而是并发的三色标记清除。为什么一定要「写屏障」？把 GOGC 调小真的更快吗？容器里明明还有余量却被 OOM kill 又是为什么？这一讲从三色不变式出发，把每个结论落到 gctrace 的输出和 runtime 的具体字段上。

## 这一讲你会得到什么

<CheckList id="L33-goals" title="本讲目标" :items='["用三色不变式解释漏标为什么会发生","说清插入屏障、删除屏障与混合写屏障的动机","读懂 gctrace 每行输出并区分两处 STW","在 GOGC 与 MemoryLimit 之间做出有依据的取舍","写出指针密度更低的 GC 友好代码"]' />

## 1. 先建立直觉

把堆想成一张有向图，回收就是「从根出发找不到的节点都清掉」。三色标记给每个节点涂色：白色是还没碰过的候选垃圾，灰色是已确认存活但孩子还没扫完，黑色是自己和孩子都扫完的。标记过程就是把灰节点不断推进成黑、把它的白孩子染灰，直到没有灰节点，剩下的白色就是垃圾。

<ConceptDiagram type="gc-tricolor" title="三色标记：白 → 灰 → 黑" caption="灰对象是边界，黑对象不再被扫描，标记结束时白对象即可回收" />

难点在于 Go 让 mutator（你的 goroutine）和 collector 同时跑。若标记期间把一个白色对象挂到已变黑的对象上，同时删掉从灰色对象通往它的那条引用，这个存活对象就再也不会被染灰，标记结束时会被误删，后果是悬垂指针或数据损坏。写屏障正是为堵住这个漏洞而存在的钩子。

<Callout type="idea" title="先记住这条不变量" text="强三色不变式：黑色对象绝不指向白色对象。弱三色不变式：黑色可以指向白色，但该白色必须仍能从某个灰色对象可达。任一成立，漏标就不会发生。" />

## 2. 语法与规则

先让 GC 真的跑起来，再用命令看它的轨迹。

```go
package main

import (
	"fmt"
	"runtime"
)

var sinkValue byte

func workload() {
	var sum byte
	for i := 0; i < 20; i++ {
		buf := make([]byte, 4<<20) // 每轮约 4 MiB 的短命对象
		buf[0] = byte(i)
		sum += buf[0] + buf[len(buf)-1]
	}
	sinkValue = sum // 只保留标量，20 轮大对象很快全成垃圾
}

func main() {
	workload()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("NumGC=%d, HeapAlloc=%d MiB, 累计暂停=%d ms\n",
		m.NumGC, m.HeapAlloc>>20, m.PauseTotalNs/1e6)
}
```

命令：`GODEBUG=gctrace=1 go run gc.go`。每行形如 `gc 1 @0.004s 3%: 0.02+0.9+0.01 ms clock, 4 → 5 → 3 MB, 5 MB goal, 8 P`，其中 `4 → 5 → 3 MB` 依次是标记开始堆、标记结束堆、存活堆，`goal` 就是下一轮触发目标。

<OutputBox title="gctrace 一行拆解" output="gc 3 @0.012s 5%: 0.01+1.2+0.02 ms clock ... 4 → 5 → 3 MB, 6 MB goal, 8 P\n0.01 = 标记开始 STW；1.2 = 并发标记；0.02 = 标记终止 STW\n4 → 5 → 3 依次是标记开始堆、标记结束堆、存活堆\n5%  = 本轮 GC 占用的 CPU 时间比例" note="真正「世界暂停」的只有开头与结尾两小段，中间标记与 mutator 并发执行。" />

<LineByLine code="var m runtime.MemStats\nruntime.ReadMemStats(&m)\nfmt.Println(m.NumGC)" :notes='["声明堆统计结构，字段含 NumGC、HeapAlloc、PauseTotalNs","把当前 GC 统计快照读进结构体","打印累计 GC 轮次，用于核对 gctrace 的行数"]' />

## 3. 深入一步

插入屏障、删除屏障与混合写屏障，本质是堵住漏标的两个条件之一。漏标必须同时满足：黑色对象新增了指向白色对象的引用，且灰色对象到该白色对象的路径被切断。Dijkstra 插入屏障在新引用写入时把「新指向的白对象」染灰，维护强不变式；Yuasa 删除屏障在旧引用被覆盖时把「原来指向的对象」染灰，维护弱不变式。两者都要求对栈做处理，而栈上对象数量大、写频繁，逐次加屏障代价高。

Go 1.8 起采用混合写屏障（hybrid write barrier）：在 `writePointer` 里同时把「被覆盖的旧值」和「新写入的值」染灰，并在标记开始的短暂 STW 中把各 goroutine 的栈一次性扫黑、之后栈上的写不再加屏障。这样标记终止阶段就不必再 STW 重扫栈，停顿被压到亚毫秒级。相关实现见 `runtime/mbarrier.go` 的 `wbBufFlush` 与 `runtime/mgcmark.go`。再加上辅助标记（mutator assist）：分配太快的 goroutine 会被临时拉去帮忙标记，避免堆在标记期间失控膨胀。

<FlowTimeline :steps='[{"title":"标记开始 STW","desc":"开启写屏障、扫描根、把栈一次性变黑","tone":"red"},{"title":"并发标记","desc":"后台 worker 推进灰对象，mutator 通过屏障协助","tone":"blue"},{"title":"标记终止 STW","desc":"关闭写屏障、完成统计、计算下一轮 goal","tone":"red"},{"title":"并发清扫","desc":"回收白色 span，按 size class 归还缓存","tone":"green"}]' direction="horizontal" />

下面用同一份负载对比三种配置，命令统一为 `GODEBUG=gctrace=1 go run tune.go`。

```go
package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

var sinkValue byte

func workload() {
	var sum byte
	for i := 0; i < 50; i++ {
		buf := make([]byte, 1<<20)
		buf[0] = byte(i)
		sum += buf[0] + buf[len(buf)-1]
	}
	sinkValue = sum
}

func report(tag string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("%s: NumGC=%d, HeapAlloc=%d MiB, 累计暂停=%d ms\n",
		tag, m.NumGC, m.HeapAlloc>>20, m.PauseTotalNs/1e6)
}

func main() {
	workload()
	report("GOGC=100")

	debug.SetGCPercent(20) // 堆增长 20% 就触发：堆更小、GC 更频繁
	workload()
	report("GOGC=20 ")

	debug.SetMemoryLimit(48 << 20) // 软上限 48 MiB，Go 1.19+
	workload()
	report("GOGC=20+Limit")
}
```

触发公式是 `goal = live × (1 + GOGC/100)`，这是个相对目标；`debug.SetMemoryLimit`（等价环境变量 GOMEMLIMIT）是绝对软上限，把堆、栈与其它运行时内存一并纳入约束。两者取「更激进者」：接近上限时 GC 会不顾 GOGC 的节奏提前介入。

| 维度 | GOGC | MemoryLimit / GOMEMLIMIT |
| --- | --- | --- |
| 性质 | 相对比例，随存活堆浮动 | 绝对软上限，含全部 runtime 内存 |
| 调小效果 | 堆更小、GC 更频繁、CPU 更高 | 逼近上限时 GC 高频，防止 OOM |
| 适用场景 | 通用吞吐与延迟权衡 | 容器内存配额固定、防 OOM |
| 风险 | 调太大堆膨胀、调太小 CPU 被吃 | 长期贴限会 GC thrashing |

<Term word="写屏障" text="编译器在指针写操作处插入的一小段钩子，用于在并发标记期间维护三色不变式、防止存活对象被漏标。" />
<Term word="辅助标记" text="mutator assist，分配过快时让当前 goroutine 分担标记工作，抑制标记期间的堆增长。" />

<Callout type="warn" title="易错点" text="GOGC=off 只是关掉按比例的触发，MemoryLimit 与手动 runtime.GC 仍会生效；另外 MemoryLimit 是软上限，不是硬保留，突发的不可回收分配仍可能短暂越界。别把 gctrace 里的 goal 当成实际内存上限。" />

### 推导链：从三色不变式推到混合屏障与调步公式

结论落在 `runtime/mgc.go` 的 `gcStart`、`gcMarkDone`、`gcMarkTermination`、`gcBgMarkWorker` 与 `gcController`（类型 `gcControllerState`，字段 `triggerRatio`、`heapGoal`、`gcPercent`、`memoryLimit`），`runtime/mbarrier.go` 的 `gcWriteBarrier`/`wbBuf`/`wbBufFlush`，以及 `runtime/mgcmark.go` 的 `gcDrain`/`markroot`。Go 1.8 起用混合写屏障，Go 1.19 起才有 `SetMemoryLimit`。推导链分四步：

- **漏标的两个条件**：黑色对象新增了指向白色对象的引用，且灰色对象到该白色对象的路径被切断，二者同时成立才会误删存活对象。Dijkstra 插入屏障染灰「新指向的白值」维护强不变式，Yuasa 删除屏障染灰「被覆盖的旧值」维护弱不变式；两者都要对栈上的写加屏障，而栈写频繁、对象多，代价高。
- **混合屏障**：`gcWriteBarrier` 把「被覆盖的旧值」与「新写入的值」都推进 `wbBuf`，缓冲满时 `wbBufFlush` 交给标记队列；配合标记开始 STW 里把各 goroutine 的栈一次性扫黑，之后栈上写不再加屏障，标记终止就不必再 STW 重扫栈。这不是优化选择，而是并发标记的正确性前提。
- **调步公式**：`goal = live × (1 + GOGC/100)` 由 `gcController` 的 `triggerRatio` 与 `heapGoal` 计算，是相对目标；`debug.SetMemoryLimit`（等价 `GOMEMLIMIT`）是纳入堆、栈与 runtime 内存的绝对软上限，两者取更激进者，逼近上限时 GC 会不顾 GOGC 节奏提前介入。
- **辅助标记**：分配速率超过后台 worker 处理能力时，当前 goroutine 被临时拉去 `gcDrain` 一部分工作，抑制标记期间的堆膨胀。

用 `runtime/metrics` 把「goal 不等于进程上限」量化出来：

```go
package main

import (
	"fmt"
	"runtime/debug"
	"runtime/metrics"
)

func main() {
	debug.SetGCPercent(50)
	var sink [][]byte
	for i := 0; i < 40; i++ {
		sink = append(sink, make([]byte, 1<<20))
	}
	names := []string{
		"/gc/heap/goal:bytes",
		"/gc/heap/objects:objects",
		"/memory/classes/heap/objects:bytes",
		"/memory/classes/total:bytes",
	}
	samples := make([]metrics.Sample, len(names))
	for i, n := range names {
		samples[i].Name = n
	}
	metrics.Read(samples)
	for _, s := range samples {
		fmt.Printf("%s = %d\n", s.Name, s.Value.Uint64())
	}
	_ = sink
}
```

```bash
go run metrics.go
GODEBUG=gctrace=1 go run metrics.go
```

<OutputBox title="metrics.go 输出（Go 1.23 / arm64，数值随机型波动）" output="/gc/heap/goal:bytes = 61750392\n/gc/heap/objects:objects = 410\n/memory/classes/heap/objects:bytes = 42153872\n/memory/classes/total:bytes = 49040400" note="total 大于堆对象字节，说明 goal 不是进程内存上限。" />

逐字段解释：`/gc/heap/goal:bytes` 是 `gcController.heapGoal` 的瞬时值，正对应 `GODEBUG=gctrace=1` 每行末尾的 `MB goal`；`/gc/heap/objects:objects` 是存活对象数；`/memory/classes/heap/objects:bytes` 是堆对象占用的字节；`/memory/classes/total:bytes` 是 runtime 向操作系统申请的全部内存，包含栈、GC 元数据等，因此必然大于堆对象字节——这正是「goal 不是进程内存上限、容器配额必须另设 GOMEMLIMIT」的量化证据。gctrace 每行里用加号分隔的三段分别是标记开始 STW、并发标记、标记终止 STW，箭头串起的是标记开始堆、标记结束堆与存活堆，末尾百分比是 GC 占用的 CPU 比例而非停顿。观测局限：metrics 是采样点快照、`gctrace` 时间精度有限、`PauseTotalNs` 是累计值，三者都不能替代容器层面的 RSS 监控。

**为什么这样设计**：不用引用计数，是因为循环引用无法回收、每次赋值都要增减计数（热路径写放大）、且无法批量并发回收。不做分代，是因为逃逸分析已让大量短命对象根本不进堆（栈上随函数返回回收），分代收益被削弱；且分代需要跨代写屏障记录、移动对象要更新所有指针，与 Go 的非移动 GC 与 cgo 指针约束冲突。代价是每轮都要扫全部存活堆，所以「降指针密度、让 span 标 noscan」才那么关键。并发标记加两处 STW，是因为完全 STW 的停顿随堆线性增长；把主体工作摊给 mutator 与后台 worker，只在「开启屏障加扫根」和「关闭屏障加收尾」两处短暂暂停。替代方案（并发却不设屏障）必然漏标，屏障是正确性要求而非性能开关。

### 触发比值的完整计算：GC 其实在 goal 之前就启动

上一节给出 `goal = live × (1 + GOGC/100)`，但把它当成「堆涨到 goal 才触发」就漏了最关键的一步。`runtime/mgcpacer.go` 的 `gcControllerState` 里，因果链是：`debug.SetGCPercent(p)` 写入 `gcPercent` → `commit` 重算 `gcPercentHeapGoal = heapMarked × (1 + gcPercent/100)`（GOGC 为负时置 `^uint64(0)` 哨兵）→ `heapGoal()` 在它与 `memoryLimit` 推导的目标里取**更激进者** → `trigger()` 并不把 goal 本身当触发点，而是算一个区间：`triggerLowerBound = ((goal - heapMarked)/64)×45 + heapMarked`、`maxTrigger = ((goal - heapMarked)/64)×61 + heapMarked`（常量 `triggerRatioDen=64`、`minTriggerRatioNum=45`、`maxTriggerRatioNum=61`）→ `gcTrigger.test()` 比较 `heapLive.Load() >= trigger` 才调 `gcStart`。另有两条触发源：`gcTriggerTime`（距上次 GC 超过 `forcegcperiod = 2 分钟` 且 gcPercent 非负）与 `gcTriggerCycle`（`runtime.GC()` 手动强制）。之所以提前，是因为 `gcBackgroundUtilization = 0.25`：后台标记 worker 只占约四分之一 CPU，必须留出并发标记的时间窗，堆不能真涨到 goal 才开始。

代入一组数：设上轮标记存活量 `heapMarked = 100 MiB`、`GOGC=100`，则 `goal = 100 × 2 = 200 MiB`、`Δ = 100 MiB`，触发区间是 `[100 + 45/64×100, 100 + 61/64×100] = [170.3, 195.3] MiB`；`GOGC=20` 时 `goal=120 MiB`、`Δ=20 MiB`，触发区间 `[114.1, 119.1] MiB`。把 `GOGC` 逐档改掉并读 `/gc/heap/goal:bytes`，能看到 goal 随之单调变化、关掉比例触发后变成极大哨兵值：

```go
package main

import (
	"fmt"
	"runtime/debug"
	"runtime/metrics"
)

var sink [][]byte

func main() {
	for _, pct := range []int{50, 100, 200, -1} {
		debug.SetGCPercent(pct)
		live := make([][]byte, 40)
		for i := range live {
			live[i] = make([]byte, 1<<20)
		}
		s := make([]metrics.Sample, 1)
		s[0].Name = "/gc/heap/goal:bytes"
		metrics.Read(s)
		g := s[0].Value.Uint64()
		if pct < 0 {
			fmt.Printf("GOGC= off → goal=%d（哨兵值，约 7 EiB）\n", g)
		} else {
			fmt.Printf("GOGC=%4d → goal=%d MiB\n", pct, g>>20)
		}
		sink = live
	}
}
```

```bash
go run goal.go
GODEBUG=gctrace=1 go run goal.go 2>&1 | grep '^gc' | head -3
```

<OutputBox title="goal 实测与 gctrace 一行（Go 1.23 / arm64，数值随堆状态波动）" output="GOGC=  50 → goal=58 MiB\nGOGC= 100 → goal=158 MiB\nGOGC= 200 → goal=237 MiB\nGOGC= off → goal=8946670875745167059（哨兵值，约 7 EiB）\n--- gctrace 一行（GOGC=100 阶段）---\ngc 3 @0.024s 0%: ... 14 → 14 → 8 MB, 17 MB goal" note="goal 随 GOGC 单调增大；off 把比例触发换成极大哨兵值，只看量级不看个位。" />

逐字段解释：`/gc/heap/goal:bytes` 就是 `gcController.heapGoal()` 的瞬时值，正对应 `gctrace` 每行末尾的 `MB goal`；三档 GOGC 下 goal 为 58、158、237 MiB，随比例增大而增大，但**不等于** `live × (1+GOGC/100)` 的简单代入——因为每轮循环都在分配新的 40 MiB，`heapMarked` 在变，且最小堆与调步修正会参与计算。`GOGC=off` 把 `gcPercentHeapGoal` 置为 `^uint64(0)` 一类的哨兵，实测约 7 EiB，等于「按比例的触发被关掉」，但 `MemoryLimit` 与手动 `runtime.GC()` 仍生效。`gctrace` 那行里 `8 MB` 是存活堆、`17 MB goal` 是下一轮目标，`8 × 2 = 16` 与 17 的差就是调步修正，说明 goal 是相对目标而非硬上限。

**反直觉结论的证伪之一**：「堆涨到 goal 那一刻才触发 GC」是错的，实际触发点在 goal 的约 70% 到 95% 处，为并发标记留时间；`gctrace` 里的 goal 是**下一轮**目标，不是本轮触发点，别把两者当成同一个数。之二针对上一节的漏标条件：「黑色对象指向白色对象就必然漏标」也是错的。漏标必须**同时**满足两个条件——黑色对象新增了指向白色对象的引用，且灰色对象到该白色对象的路径被切断。四种组合如下：

| 黑对象新增指向白对象的引用 | 灰对象到白对象的路径被切断 | 是否漏标 | 屏障怎么挡 |
| --- | --- | --- | --- |
| 否 | 否 | 否 | 无需屏障 |
| 是 | 否 | 否，弱三色不变式仍成立，灰对象稍后会把白对象染灰 | 插入屏障（染灰新值）已足够 |
| 否 | 是 | 否，该白对象本就不可达、是垃圾 | 删除屏障（染灰旧值）已足够 |
| 是 | 是 | **是**，白色对象再无灰色路径可达 | 必须两者都挡，即混合屏障 |

Dijkstra 插入屏障只堵第一列，Yuasa 删除屏障只堵第二列，Go 1.8 起的混合屏障把「被覆盖的旧值」与「新写入的值」都染灰，两列一起堵；配合标记开始 STW 里把栈一次扫黑、之后栈上写不加屏障，才把标记终止的重扫栈省掉。这也说明屏障是**正确性前提**而非性能开关：单独关掉任意一列，第四行的组合就会误删存活对象。

边界：`triggerRatio*` 与 `gcBackgroundUtilization` 是 Go 1.23 的取值，调步算法随版本调整；`heapMarked` 是上轮标记存活量而非当前 `HeapAlloc`，别拿 `MemStats` 的瞬时值去套公式；小堆程序还受最小堆约 4 MiB 的下限影响，实测会偏离代入值；`MemoryLimit` 自 Go 1.19 起、混合写屏障自 Go 1.8 起；`metrics` 是采样快照、`gctrace` 时间精度有限，`PauseTotalNs` 是累计值，三者都不能替代容器层面的 RSS 监控。

### 辅助标记的换算：`assistWorkPerByte` 把分配字节变成扫描债务

第 3 节说过「分配太快的 goroutine 被拉去帮忙标记」，但没给出「帮多少」的算式。它在 `runtime/mgcpacer.go` 的 `gcControllerState.revise()` 里是一个除法，字段名就叫 `assistWorkPerByte`：

```text
scanWorkRemaining = max(scanWorkExpected - work, 1000)   // 剩余扫描工作量，下限 1000
heapRemaining     = max(heapGoal - live, 1)              // 距离目标还能分配多少字节，下限 1
assistWorkPerByte = scanWorkRemaining / heapRemaining    // 每分配 1 字节该分担多少扫描工作
assistBytesPerWork = heapRemaining / scanWorkRemaining   // 上面的倒数
```

两个值用 `atomic.Float64.Store` 分别写入，源码注释特意说明「不是一起原子更新、允许轻微偏斜，因为它们在一轮 GC 里变化很慢」。设计意图写在注释里：让 mutator 在把堆从 `live` 分配到 `heapGoal` 的这段路上，**刚好把剩余的 `scanWorkRemaining` 做完或「偷」完**；所以比值是「剩余工作量除以剩余可分配量」，而不是一个拍脑袋的常数。

消费端在 `runtime/mgcmark.go` 的 `gcAssistAlloc(gp)`：

```text
debtBytes = -gp.gcAssistBytes                       // 本 goroutine 累积的分配债务
scanWork  = int64(assistWorkPerByte * debtBytes)     // 换算成扫描工作量
若 scanWork 小于 gcOverAssistWork(64 左移 10 = 65536)：
    scanWork = 65536                                // 少量债务也多做一些，摊薄后续调用
    debtBytes = int64(assistBytesPerWork * scanWork)
先从 gcController.bgScanCredit 偷后台 worker 的余量，偷到的从 scanWork 扣掉
剩余 scanWork 交给 gcAssistAlloc1 → gcDrainN(gcw, scanWork) 真正扫对象
回收：gp.gcAssistBytes += 1 + int64(assistBytesPerWork * workDone)
```

代入一组数：设 `heapGoal - live = 8 MiB = 8388608` 字节、`scanWorkRemaining = 4 MiB = 4194304` 个扫描单位，则 `assistWorkPerByte = 0.5`、`assistBytesPerWork = 2`。一个 goroutine 一次分配 1 MiB 就欠下 `1048576 × 0.5 = 524288` 个扫描单位；若只分配 4 KiB，算出 `2048` 单位小于 65536，走「过度辅助」分支，实际扫 65536 单位并把债务按 `assistBytesPerWork` 折算回去。于是「分配越快、被拉去标记越久」不是定性描述，而是 `assistWorkPerByte × 分配字节` 这条乘法；`gcOverAssistWork` 的下限保证极小额分配不会每次都进出辅助路径。

新观察角度：不用 `gctrace`、也不读 `runtime/metrics`，直接读**源码算式**并反汇编辅助标记的终点。存成 `assist.go`（只需一个持续分配的程序，用于让链接器保留这些符号）：

```go
package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

var hold [][]byte

func main() {
	debug.SetGCPercent(20)
	for i := 0; i < 40; i++ {
		hold = append(hold, make([]byte, 1<<20))
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Println("NumGC:", m.NumGC, "NextGC MiB:", m.NextGC>>20)
}
```

```bash
go build -o assistbin assist.go
go tool objdump -s '^runtime\.gcAssistAlloc1' ./assistbin | grep -E 'TEXT|gcDrainN|gcWriteBarrier'
sed -n '555,595p' "$(go env GOROOT)/src/runtime/mgcpacer.go"
sed -n '465,480p' "$(go env GOROOT)/src/runtime/mgcmark.go"
sed -n '50,56p' "$(go env GOROOT)/src/runtime/mgcpacer.go"
```

<OutputBox title="辅助比公式与终点符号（Go 1.23.8 / darwin arm64）" output="TEXT runtime.gcAssistAlloc1(SB) runtime/mgcmark.go\n  mgcmark.go:653  CALL runtime.gcDrainN(SB)\n--- mgcpacer.go revise() ---\nassistWorkPerByte := float64(scanWorkRemaining) / float64(heapRemaining)\nassistBytesPerWork := float64(heapRemaining) / float64(scanWorkRemaining)\n--- mgcmark.go gcAssistAlloc() ---\nscanWork := int64(assistWorkPerByte * float64(debtBytes))\nif scanWork 小于 gcOverAssistWork {\n--- mgcpacer.go 常量 ---\ngcOverAssistWork = 64 左移 10" note="乘法在 mgcmark.go，除法在 mgcpacer.go，终点是 gcDrainN。" />

逐字段解释：`mgcpacer.go` 那两行把 `assistWorkPerByte` 与倒数 `assistBytesPerWork` 摆在同一处，验证「比值来自剩余工作量与剩余堆距离的商」；`mgcmark.go` 的 `scanWork := int64(assistWorkPerByte * float64(debtBytes))` 是债务换算的那次乘法；`gcOverAssistWork = 64 << 10` 给出下限 65536 的字面出处；`go tool objdump` 里 `mgcmark.go:653` 的 `CALL runtime.gcDrainN(SB)` 是整条链的终点——辅助标记最终落到 `gcDrainN` 扫描对象，而不是某个抽象计数器。注意 `-gcflags` 类开关看不到运行时的这两个原子字段，只有源码与链接产物的符号能证据化；`go tool objdump` 的地址随构建变化，只比符号与源码行号。

边界：`scanWorkRemaining` 下限 1000、`heapRemaining` 下限 1、`gcOverAssistWork` 与 `bgScanCredit` 的偷取逻辑都是 Go 1.23 的调步实现，随版本调整；`assistWorkPerByte` 与 `assistBytesPerWork` 分开原子写、允许偏斜，别把两者当成严格互为倒数去套瞬时值；`gcCPULimiter.limiting()` 为真时 `gcAssistAlloc` 会提前返回、故意不辅助，所以 CPU 受限时这条链会断开；`//go:noinline` 与内联不影响运行时符号，但样本太小可能让 `gcAssistAlloc1` 被裁掉，故示例里持续分配 40 MiB。这条链在第 96 讲（GC 源码与真实调优）会与 `gcDrain`、后台 worker 合流。

### `MemoryLimit` 怎样改写 goal：从 `memoryLimitHeapGoal` 到可读的 `NextGC`

上一版已经推过 `goal = live × (1 + GOGC/100)` 与提前触发区间，这里不重复，只补「绝对软上限」那半条链。`runtime/mgcpacer.go` 的 `heapGoalInternal()` 第一句就定下了组合规则：

```text
goal = gcPercentHeapGoal                       // 按 GOGC 算出的相对目标
若 memoryLimitHeapGoal() 小于 goal → goal = 它 // 取更激进者
```

`memoryLimitHeapGoal()` 本身是一串可代入的减法（`mgcpacer.go`）：

```text
nonHeapMemory = mappedReady - heapFree - heapAlloc       // 非堆 runtime 内存
overage       = max(mappedReady - memoryLimit, 0)
若 nonHeapMemory + overage 不小于 memoryLimit → 返回 heapMarked（最低目标，只能靠 CPU limiter 兜）
goal          = memoryLimit - (nonHeapMemory + overage)
headroom      = max(goal / 100 * 3, 1 左移 20)           // 3% 或至少 1 MiB
goal          = goal - headroom
```

常量出处：`memoryLimitHeapGoalHeadroomPercent = 3`、`memoryLimitMinHeapGoalHeadroom = 1 << 20`。所以 `MemoryLimit` 不是「把 goal 直接设成配额」，而是「配额先扣掉非堆内存、已越界部分与 3% 余量」，再与 GOGC 目标取较小值。这也解释了 `GOGC=off` 时 `MemoryLimit` 仍生效：`gcPercentHeapGoal` 变成极大哨兵，`min` 必然选中 `memoryLimitHeapGoal()`。

新观察角度：用 `runtime.ReadMemStats` 的 `NextGC` 字段把这半条链量出来——它是 `gcController.heapGoal()` 的对外读数，不需要 `runtime/metrics`，也不依赖 `gctrace` 的行解析。存成 `memlim.go`：

```go
package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

var live [][]byte

func nextGC() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.NextGC
}

func main() {
	live = make([][]byte, 20)
	for i := range live {
		live[i] = make([]byte, 1<<20)
	}
	fmt.Println("20 MiB 存活, GOGC=100 NextGC =", nextGC()>>20, "MiB")
	debug.SetMemoryLimit(40 << 20)
	fmt.Println("Limit=40MiB      NextGC =", nextGC()>>20, "MiB")
	debug.SetMemoryLimit(24 << 20)
	fmt.Println("Limit=24MiB      NextGC =", nextGC()>>20, "MiB")
	_ = live
}
```

```bash
go run memlim.go
sed -n '900,916p' "$(go env GOROOT)/src/runtime/mgcpacer.go"
sed -n '1026,1060p' "$(go env GOROOT)/src/runtime/mgcpacer.go"
```

<OutputBox title="NextGC 随 MemoryLimit 收紧（Go 1.23.8 / darwin arm64）" output="20 MiB 存活, GOGC=100 NextGC = 36 MiB\nLimit=40MiB      NextGC = 35 MiB\nLimit=24MiB      NextGC = 19 MiB\n--- heapGoalInternal() ---\ngoal = c.gcPercentHeapGoal.Load()\nif newGoal := c.memoryLimitHeapGoal(); newGoal 小于 goal {\n    goal = newGoal\n}\n--- memoryLimitHeapGoal() ---\ngoal := memoryLimit - (nonHeapMemory + overage)\nheadroom := goal / 100 * memoryLimitHeapGoalHeadroomPercent" note="40 MiB 配额几乎不改 goal，24 MiB 配额把 goal 从 36 压到 19 MiB。" />

逐字段解释：第一步先造 20 MiB 存活堆，避免最小堆 4 MiB 把 `NextGC` 压成小数值、`>>20` 后全变成 0；此时 `GOGC=100` 的 `NextGC` 是 36 MiB（存活 20 MiB 的相对目标加调步修正）。`Limit=40MiB` 时配额宽松，`memoryLimitHeapGoal()` 算出的值约 35 MiB、仍大于或接近 GOGC 目标，`min` 选择基本不变；`Limit=24MiB` 时 `goal = 24MiB - nonHeapMemory - 3% 余量` 落到约 19 MiB，`min` 选中它，`NextGC` 从 36 掉到 19 MiB——这就是「逼近上限时 GC 不顾 GOGC 节奏提前介入」的可读证据。`sed` 两段给出 `heapGoalInternal()` 的 `min` 与 `memoryLimitHeapGoal()` 的减法/余量原文，把数值变化接回源码。

边界：`NextGC` 是采样点瞬时值，`SetMemoryLimit` 后要等 `commit` 重算才稳定，连续读可能看到中间态；`>>20` 会把不足 1 MiB 的部分截断，小堆实验要先造存活量；`memoryLimit` 是软上限，`nonHeapMemory + overage` 超过配额时函数直接返回 `heapMarked` 并靠 CPU limiter 抑制，此时数值不再随配额线性变化；`headroom` 有 1 MiB 下限，配额很小时实测会偏离代入值；常量与 `mappedReady`/`heapFree` 的口径都是 Go 1.23 实现细节；`ReadMemStats` 会短暂 STW，别放进热路径。这条链与上一节的 `assistWorkPerByte` 合起来才完整：限额压低 `heapGoal` → `heapRemaining` 变小 → `revise()` 算出的 `assistWorkPerByte` 变大 → 同样分配量摊到更多辅助标记，这正是「贴限运行 CPU 占比升高」的量化来源。

### 边界、常见误解与前后联系

| 常见误解 | 正解 | 可验证依据 |
| --- | --- | --- |
| GOGC 调小等于更快 | 堆更小但 GC 更频繁、CPU 占比更高、吞吐下降；它换的是内存不是速度 | 第 3 节三种配置的 NumGC 与 PauseTotalNs 对比 |
| gctrace 的 goal 就是进程内存上限，不会 OOM | 是相对触发目标、不感知容器配额；固定配额要用 `GOMEMLIMIT` | 上表与 `metrics.go` 的 total 字段 |
| 写屏障只在 GC 期间有开销 | 屏障代码由编译器常驻插入，只是 GC 期间才真正染灰；非 GC 期也有条件判断 | `go build -gcflags=-S` 里的写屏障调用点 |

边界要点：`GOGC=off` 只关掉按比例触发，`MemoryLimit` 与手动 `runtime.GC()` 仍生效；`MemoryLimit` 是软上限，突发的不可回收分配仍可能短暂越界；后台标记 worker 数量约 `GOMAXPROCS` 的四分之一，故核数与平台会改变并发度；混合写屏障自 Go 1.8 起，更早版本在标记终止要 STW 重扫栈；`runtime.GC()` 是阻塞式的、会额外触发一轮，不该放进热路径；`gctrace` 的 `5%` 是 CPU 占比、`goal` 是触发目标，二者都别当成内存上限。

前后联系：第 27 讲（逃逸分析）决定对象是否进堆，第 32 讲（分配器）决定 span 是否 noscan，本讲决定它们何时被回收；第 96 讲（GC 源码与真实调优案例）从 `gcStart` 到 `gcMarkDone` 精读一次 GC（后台 worker、辅助标记、调步算法、GOGC/MemoryLimit 实战），是本讲最直接的源码延续，第 95 讲（调度器源码）讲后台标记 worker 如何被调度与抢占，第 100 讲（极致性能优化）把「减分配 → 降指针密度 → 最后动 GOGC」作为标准闭环。

## 4. 工程实践

GC 的 CPU 大头是「扫描指针」。同样是存 20 万个节点，用指针切片会让每个元素都成为待扫描引用；用值切片则整块内存可被标为 noscan，标记阶段直接跳过。

<Compare :panels='[{"label":"指针密集","code":"keep = make([]*Node, n)\nfor i := 0; i != n; i++ { keep[i] = new(Node) }","note":"n 次分配，scan span，逐个扫描指针"},{"label":"值连续","code":"keep = make([]Node, n)\nfor i := range keep { keep[i].ID = i }","note":"1 次分配，noscan span，标记阶段跳过"}]' title="降低指针密度" />

第三段代码把这个差异测出来：

```go
package main

import (
	"fmt"
	"runtime"
	"unsafe"
)

type PtrNode struct { // 含指针：GC 必须递归扫描
	Next *PtrNode
	Val  int64
}
type ValNode struct { // 无指针：分配器打 noscan，GC 完全跳过
	Idx int32
	Val int64
}

var keepP []*PtrNode
var keepV []ValNode

func main() {
	const N = 200000
	keepP = make([]*PtrNode, N)
	keepV = make([]ValNode, N)
	for i := range keepP {
		keepP[i] = &PtrNode{Val: int64(i)}
		keepV[i] = ValNode{Idx: int32(i), Val: int64(i)}
	}
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("PtrNode=%d B, ValNode=%d B, HeapObjects=%d, 目标=%d MiB\n",
		unsafe.Sizeof(PtrNode{}), unsafe.Sizeof(ValNode{}), m.HeapObjects, m.HeapAlloc>>20)
}
```

命令：`GODEBUG=gctrace=1 go run friendly.go` 对比标记段耗时，`go build -gcflags=-m friendly.go` 看逃逸。工程上的次序是：先减少分配次数（预分配、复用），再降低指针密度，最后才动 GOGC，前者治本，后者只是调步。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l3-core/lesson-033-gc`](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-033-gc)。clone 仓库后进入 `examples/l3-core`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="读一次 gctrace" task="用 GODEBUG=gctrace=1 跑 workload 程序，找出 goal 随 GOGC 从 100 调到 20 后的变化，并解释为什么累计暂停时间上升、单次暂停反而变短。" :hints='["关注每行的 MB goal 数值","对比 NumGC 与 PauseTotalNs","结合 goal = live × (1 + GOGC/100)"]' solution="GOGC=100 时 goal 约为存活堆的 2 倍，GC 次数少；\nGOGC=20 时 goal 约为存活堆的 1.2 倍，GC 更频繁，\n故累计暂停上升，但每轮存活集更小、单次标记更短。" explain="目标变小意味着更早触发，堆被压得更低；代价是 GC 轮次增加、CPU 占比上升。" />

## 随堂检测

<Quiz no="33-1" q="混合写屏障最主要解决了什么问题？" :options='["让 GC 变成分代回收","避免标记终止阶段 STW 重新扫描所有栈","把对象从堆移到栈","关闭辅助标记"]' answer="1" explain="旧方案在标记终止要 STW 重扫栈；混合屏障配合标记开始时把栈一次性扫黑，使终止 STW 只需收尾，停顿降到亚毫秒。" />
<Predict no="33-2" q="容器内存配额 64 MiB、存活堆很小，但程序疯狂分配不可回收对象。只设 GOGC=100 最可能怎样？" :options='["GC 变少、吞吐更高","堆按 goal 增长直到逼近配额，可能被 OOM kill","MemoryLimit 自动生效","暂停时间恒定"]' answer="1" output="堆随分配不断向 goal 增长，容易触碰容器配额而被 OOM kill" explain="GOGC 是相对目标，不感知容器配额；此时应设 GOMEMLIMIT 让 GC 为守住绝对上限而提前介入。" />

## 一页纸速记

<KnowledgeCard :points='["三色：白候选、灰边界、黑已完成；标记结束剩余白色即垃圾","漏标需同时满足「黑指向白」与「灰到白的路径断开」，写屏障堵其一","Go 1.8+ 混合写屏障把新旧值都染灰，配合栈一次扫黑，消除终止 STW 重扫栈","GOGC 是相对目标，MemoryLimit 是绝对软上限，取更激进者","扫描成本取决于指针密度，noscan span 会被标记阶段跳过"]' :pitfalls='["把 gctrace 的 goal 当硬内存上限","一上来就调 GOGC，忽视分配次数与指针密度这两个治本项"]' summary="GC 的本质是并发可达性遍历，写屏障维持正确性，GOGC 与 MemoryLimit 决定节奏。" />

## 小结与下一讲

- 三色不变式是理解写屏障的钥匙，混合屏障是 Go 把停顿压到亚毫秒的关键。
- `GODEBUG=gctrace=1` 能同时看到两处 STW、并发标记与 goal 计算。
- 调优顺序：减分配、降指针密度，最后才动 GOGC / GOMEMLIMIT。

GC 之外，还有一个每天用却很少追问的语法是 defer。[下一讲：defer 实现与 open-coded 优化](/core/defer-internals)
