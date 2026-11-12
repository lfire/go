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
