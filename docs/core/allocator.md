---
title: 第 32 讲 · 内存分配器：mcache / mcentral / mheap
description: 用 size class、span 与三级缓存拆解 mallocgc 的三条路径，并通过 MemStats 与 gctrace 验证分配行为
---

<StageHeader stage="L3 · 核心机制" level="★★★★☆" time="45 分钟" goal="建立 Go 内存分配全景" prereq="第 31 讲 · 字符串底层与零拷贝转换" />

# 第 32 讲 · 内存分配器：mcache / mcentral / mheap

> 上一讲我们知道了变量何时逃逸到堆。但「堆」不是一整块随意切分的内存：申请 17 字节，运行时究竟占了多少？为什么高频分配小对象几乎没有锁竞争？这一讲把 `runtime.mallocgc` 的三条路径拆开，每个结论都指向具体的数据结构字段或可复现的命令。

## 这一讲你会得到什么

<CheckList id="L32-goals" title="本讲目标" :items='["说清 size class 与就近取整的代价","画出 mcache、mcentral、mheap 三级缓存的分工","解释 tiny allocator 与大对象路径各自的动机","能用 MemStats 与 gctrace 观察并验证分配行为"]' />

## 1. 先建立直觉

把分配器想成连锁仓库：每个处理器 P 手边有一个「前台货架」mcache，取货不用排队；货架空了向「区域仓」mcentral 补一批，这一步才需要加锁；区域仓也没有，就向「总仓」mheap 申请连续页；总仓不足才通过 mmap 向操作系统要地。绝大多数小对象在前台就完成了，这正是 Go 分配快的原因。

<ConceptDiagram type="allocator" title="Go 堆分配的三级缓存与三条路径" caption="小对象本地无锁分配，缺货逐级上溯，大对象直达 mheap" />

<Callout type="idea" title="为什么分级" text="锁竞争是分配器的头号敌人。把最常用的路径做成每个 P 独占、无锁，把需要协调的路径收敛到 mcentral，Go 在多核上把分配成本压到了几乎一次指针碰撞。" />

## 2. 语法与规则

分配的第一步是「按档取整」。Go 预定义约 68 个 size class（见 `$(go env GOROOT)/src/runtime/sizeclasses.go` 的 class_to_size 表），把请求大小向上取到最近的档位，再从该档位对应的 span 里切一个 object。下面用 MemStats 观察这个现象。

```go
package main

import (
	"fmt"
	"runtime"
)

// 全局变量承接分配结果，避免被编译器判定为死代码而消除
var sink []byte

// allocOfSize 申请 n 字节，返回 HeapAlloc 的近似增量
func allocOfSize(n int) uint64 {
	runtime.GC() // 先回收上一轮残留，减少干扰
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	sink = make([]byte, n) // 逃逸到堆，必然经过分配器
	runtime.ReadMemStats(&after)
	return after.HeapAlloc - before.HeapAlloc
}

func main() {
	for _, n := range []int{1, 8, 16, 17, 32, 33, 64, 1024} {
		fmt.Printf("申请 %5d B，HeapAlloc 增量约 %5d B\n", n, allocOfSize(n))
	}
}
```

命令：`go run alloc.go`，再用 `go build -gcflags=-m alloc.go` 确认 `make([]byte, n) escapes to heap`。差值只能近似观察（运行时自身也会分配），精确的档位表要看源码。

<OutputBox title="典型 size class 对照（源码 class_to_size 摘录）" output="请求 1..8 B   → 8 B\n请求 9..16 B  → 16 B\n请求 17..24 B → 24 B\n请求 25..32 B → 32 B\n最大小对象   → 32 KB（maxSmallSize，精确边界见 malloc.go）" note="17 字节会被放到比它大的档位，多出的部分是内部碎片；这就是分档换速度的代价。" />

<LineByLine code="sink = make([]byte, n)\nruntime.ReadMemStats(&after)\nreturn after.HeapAlloc - before.HeapAlloc" :notes='["申请 n 字节，编译器按逃逸分析把它交给 runtime.mallocgc","读取分配后的堆统计快照","用差值近似观察本次分配占用的空间"]' />

## 3. 深入一步

mallocgc 按大小走三条分支：小于 16 字节且不含指针走 tiny allocator；16 字节到约 32 KB 走 size class 的 mcache；超过约 32 KB 视为大对象，按 8 KB 页取整后直接向 mheap 申请专用 span。

```go
package main

import (
	"fmt"
	"runtime"
)

type tiny struct{ b byte } // 1 字节、无指针，典型的 tiny 分配候选

var keep *tiny

func main() {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	const N = 100000
	for i := 0; i < N; i++ {
		keep = &tiny{b: byte(i)} // 地址存入全局变量，触发堆分配
	}

	runtime.ReadMemStats(&after)
	fmt.Printf("分配 %d 次，Mallocs 增量 %d，HeapAlloc 增量 %d B\n",
		N, after.Mallocs-before.Mallocs, after.HeapAlloc-before.HeapAlloc)
	fmt.Printf("平均每次约 %.2f B\n", float64(after.HeapAlloc-before.HeapAlloc)/N)
}
```

tiny allocator 把多个小于 16 字节、且不含指针的对象塞进同一个 16 字节 block，字段是 mcache 的 `tiny`、`tinyoffset` 与 `tinyAllocs`。若不合并，每个 1 字节对象也要占一个 8 字节档位，浪费严重。命令：`go run tiny.go`，平均占用应明显小于 8 字节。

```go
package main

import (
	"fmt"
	"runtime"
)

var big []byte

func main() {
	const size = 1 << 20 // 1 MiB，超过约 32 KB 的大对象直达 mheap
	big = make([]byte, size)
	big[0], big[len(big)-1] = 1, 1 // 首尾都写，确保真正占用

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("HeapAlloc=%d MiB, HeapSys=%d MiB\n", m.HeapAlloc>>20, m.HeapSys>>20)
	_ = big
}
```

命令：`GODEBUG=gctrace=1 go run big.go`，观察每次 GC 行的堆规模；大对象独占 span，不参与 mcache 的分档缓存。

<FlowTimeline :steps='[{"title":"mcache","desc":"每个 P 独占，alloc 数组按 size class 存放可用 span，无锁","tone":"blue"},{"title":"mcentral","desc":"每个 size class 一个，维护 partial 与 full span 链表，需加锁","tone":"violet"},{"title":"mheap","desc":"全局，管理 8KB 页、arena 与 span 元数据，大对象直达","tone":"green"},{"title":"OS","desc":"arena 不足时通过 mmap 向操作系统申请内存","tone":"gray"}]' direction="horizontal" />

| 结构 | 关键字段 | 作用域 | 是否加锁 |
| --- | --- | --- | --- |
| mcache | `alloc[_NumSizeClasses]*mspan`、`tiny`、`tinyoffset` | 每个 P 独占 | 否 |
| mcentral | `spanclass`、`partial`、`full` | 全局按档位共享 | 是 |
| mspan | `startAddr`、`npages`、`nelems`、`allocBits`、`gcmarkBits`、`spanclass` | 一组连续页 | 视操作而定 |
| mheap | `arenas`、`pages`、`allspans` | 全局 | 是 |

为什么不用一个通用分配器按字节任意切分？任意切分会产生外部碎片，还要维护空闲链表与相邻块合并，且每次分配都得加锁。Go 用预定义档位把「任意大小」收敛成「固定档位」，span 内部靠 allocBits 位图找空闲槽位，分配退化成一次位扫描加指针碰撞；代价是档位内的内部碎片，换来的却是无锁与可预测的常数时间。mcentral 维护 partial 与 full 两条链表也是同一思路：还有空槽的 span 能被快速补货给 mcache，全满的 span 直接摘出，避免每次扫描无效候选。

结论从哪来：档位表在 `runtime/sizeclasses.go`，分支判断在 `runtime/malloc.go` 的 `mallocgc`，span 位图在 `runtime/mheap.go`。`spanclass` 的低位还带一个 noscan 标记，标明这块 span 是否含指针，直接决定后续 GC 要不要扫描它。

<Term word="size class" text="预先定义的约 68 个尺寸档位，请求大小向上取整到最近档位，用少量内部碎片换取无锁、常数时间的分配。" />
<Term word="span" text="mspan，管理一组连续页并按固定 object 大小切分，用 allocBits 位图记录每个槽位是否空闲。" />

<Callout type="warn" title="易错点" text="MemStats 的 HeapAlloc 差值含运行时自身开销，只能做定性观察；别把它当精确公式。另注意 32 KB 是分界，且含指针的对象不会进入 tiny allocator。" />

### 深入一步：mallocgc 源码字段、实验与生产观测

下面的字段与常量以 Go 1.21~1.23 的 `$GOROOT/src/runtime` 为准，升级大版本前务必重新对照。分配入口是 `runtime/malloc.go` 的 `mallocgc`，它按请求大小分三路：小于 16 字节且类型不含指针的交给 tiny 合并，16 字节到 `maxSmallSize`（32 KB）之间按 size class 从 mcache 取槽位，更大的按 8 KB 页向 mheap 申请专用 span。这几版预定义 68 个档位（含 0 号空档），对照表在 `runtime/sizeclasses.go` 的 `class_to_size`，查它比凭印象猜可靠得多。

三级缓存各有可查字段：`runtime/mcache.go` 里 mcache 的 `alloc` 数组按档位挂可用 span，另有 `tiny`、`tinyoffset`、`tinyAllocs` 服务小对象合并；`runtime/mcentral.go` 里 mcentral 用 `spanclass` 标识档位、维护 partial 与 full 两组集合，补货才加锁；`runtime/mheap.go` 里 mspan 用 `startAddr`、`npages`、`nelems` 描述范围，用 `allocBits`、`gcmarkBits` 两张位图记占用与标记。特别留意 `spanclass` 低位的 noscan 标记：它决定 GC 是否扫描整块 span，这也解释了「能用值切片就别用指针切片」为何能削减标记阶段的工作量。

实验分三层：`go build -gcflags=-m alloc.go` 确认逃逸，`go tool compile -S alloc.go | grep -E 'mallocgc|makeslice'` 看到真实分配调用，`GODEBUG=gctrace=1 go run big.go` 则给出每次回收的堆占用与目标。线上更推荐 `runtime/metrics`（Go 1.16 起）的 `/gc/heap/allocs-by-size:bytes` 直方图，按档位分桶直接看出对象集中在哪里、碎片出在哪一档，与 pprof 的 `alloc_space` 同源。调优顺序是先减分配次数、再降指针密度、最后才动 GOGC。常见误用有三类：把 MemStats 差值当精确公式、以为含指针的小对象也能进 tiny、忽视 32 KB 分界把中等对象反复当成大对象分配。

补两处容易漏掉的机制：mcentral 的 partial 按清扫代分成两组，GC 借此区分本轮已清扫与待清扫的 span，避免重复加锁扫描；mcache 挂在 P 上，P 销毁时由调度器回收本地缓存，不会泄漏。tiny 路径另有 `tinyAllocs` 计数，供 GC 判断整个 block 是否值得回收。大对象以 `spanclass=0` 申请专用 span，不进入 mcache 档位缓存，反复申请释放会直接推高 `HeapSys` 与 GC 目标，是线上内存抖动最常见的来源之一。`needzero` 则决定新槽位是否清零。

| 请求字节 | 命中档位 / 路径 | 实际占用 | 内部碎片 | 说明 |
| --- | --- | --- | --- | --- |
| 1 | 8 B（无指针时优先 tiny 合并） | 8 B | 7 B | 多个小对象共用一个 16 B block |
| 9 | 16 B | 16 B | 7 B | 跨过 8 B 边界即跳档 |
| 17 | 24 B | 24 B | 7 B | 常见小结构体尺寸 |
| 25 | 32 B | 32 B | 7 B | 档位按 8 的倍数递增 |
| 32769 | 大对象路径，5 页 | 40960 B | 8191 B | 超过 32 KB，按 8 KB 页取整 |

<SequenceDiagram title="一次分配触发的三级上溯" caption="前台货架缺货才逐级向区域仓、总仓乃至操作系统申请" :actors='["用户代码","mallocgc","mcache","mcentral","mheap","OS"]' :steps='[{"from":"用户代码","to":"mallocgc","label":"make/new 申请","kind":"call"},{"from":"mallocgc","to":"mcache","label":"查 alloc 档位","kind":"call"},{"from":"mcache","to":"mcache","label":"位图扫描空闲槽","kind":"self"},{"from":"mcache","to":"mcentral","label":"本档 span 已空","kind":"call"},{"from":"mcentral","to":"mheap","label":"partial 也无可用","kind":"call"},{"from":"mheap","to":"OS","label":"arena 不足 mmap","kind":"call"},{"from":"OS","to":"mheap","label":"返回新页","kind":"return"},{"from":"mheap","to":"mcentral","label":"切出新 span","kind":"return"},{"from":"mcentral","to":"mcache","label":"交付可用 span","kind":"return"},{"from":"mallocgc","to":"用户代码","label":"返回 object 地址","kind":"return"}]' />

## 4. 工程实践

高频路径上，分配「次数」比分配「总量」更伤：每次都要走分档、位图与可能的补货。把零散小对象合并成一次预分配，往往比逐条 new 快一个量级。

<Compare :panels='[{"label":"逐条分配","code":"out := make([]*Item, 0, n)\nfor i := 0; i != n; i++ { out = append(out, new(Item)) }","note":"n 次分配，n 个指针，GC 需逐个扫描"},{"label":"预分配连续块","code":"buf := make([]Item, n)\nfor i := range buf { buf[i].ID = i }","note":"1 次分配，值连续，可打 noscan 标记"}]' title="减少分配次数与指针密度" />

要点：优先 `make` 预分配容量；能用值切片就别用指针切片；让大对象生命周期清晰，避免误留在长命 span 里。这些判断都能用 `go build -gcflags=-m`、`go test -bench=. -benchmem` 与 `GODEBUG=gctrace=1` 量化验证，而不是凭感觉。

### 用 runtime/metrics 看对象落在哪些档位

与其凭 MemStats 的差值猜档位，不如直接读运行时暴露的直方图。下面这段完整程序先制造几种尺寸的对象，再打印按 size class 分桶的分配次数。

```go
package main

import (
	"fmt"
	"runtime/metrics"
)

func main() {
	// 先制造不同尺寸的对象，让直方图非空
	var sink [][]byte
	for _, n := range []int{8, 24, 100, 4096, 1 << 16} {
		sink = append(sink, make([]byte, n))
	}

	// 该指标按 size class 分桶统计累计分配字节对应的次数
	const name = "/gc/heap/allocs-by-size:bytes"
	sample := []metrics.Sample{{Name: name}}
	metrics.Read(sample)
	h := sample[0].Value.Float64Histogram()
	for i, c := range h.Counts {
		if c > 0 {
			fmt.Printf("[%6.0f, %6.0f) B: %d 次\n",
				h.Buckets[i], h.Buckets[i+1], c)
		}
	}
	_ = sink
}
```

```bash
go run alloc_hist.go
GODEBUG=gctrace=1 go run alloc_hist.go
```

<OutputBox title="直方图输出（节选）" output="[      0,       8) B: 3 次\n[     16,      32) B: 2 次\n[     96,     112) B: 1 次\n[   4096,    4224) B: 1 次\n[  65536,   73728) B: 1 次" note="Buckets 是档位边界，Counts 是各桶累计分配次数；跨档即产生内部碎片。" />

`Float64Histogram` 的 `Buckets` 长度恒比 `Counts` 多一，第 i 个桶是左闭右开的 `[Buckets[i], Buckets[i+1])`，`Counts[i]` 是落进该区间的分配次数；和上表对照，就能解释「申请 100 字节却按 112 字节档位计数」。`gctrace=1` 每行里箭头后的两个数字分别是本次标记结束时的存活堆与下一次触发目标，两者长期贴近说明分配速率高、GC 频繁，正是回头查预分配与指针密度的时候。

<Quiz no="32-3" q="一个 40 KB 的 []byte 会走哪条分配路径？" :options='["tiny allocator 合并","某个 size class 从 mcache 取槽位","按 8 KB 页取整后直达 mheap","分配在栈上"]' answer="2" explain="40 KB 超过 maxSmallSize=32768，属大对象，按页取整后直接向 mheap 申请专用 span，不参与 mcache 分档缓存。" />

## 动手练一练

<Exercise title="用数据验证分档" task="把第一段代码改成对比申请 24、25、48、49 字节，记录 HeapAlloc 增量，找出发生跳档的位置，并说明内部碎片从哪来。" :hints='["先 runtime.GC 再做一次基线读取","把结果放进全局 sink，避免被优化","对照 sizeclasses.go 的 class_to_size 表"]' solution="package main\nimport (&quot;fmt&quot;; &quot;runtime&quot;)\nvar sink []byte\nfunc main() {\nfor _, n := range []int{24, 25, 48, 49} {\nruntime.GC()\nvar a, b runtime.MemStats\nruntime.ReadMemStats(&a)\nsink = make([]byte, n)\nruntime.ReadMemStats(&b)\nfmt.Println(n, b.HeapAlloc-a.HeapAlloc)\n}\n_ = sink\n}" explain="申请大小跨越档位边界时，增量会跳到下一个 size class，多出来的部分就是内部碎片。" />

## 随堂检测

<Quiz no="32-1" q="哪一个结构是每个 P 独占、因此分配小对象无需加锁？" :options='["mheap","mcentral","mcache","arena"]' answer="2" explain="mcache 挂在 P 上，alloc 数组按 size class 直接给出可用 span；只有它需要向 mcentral 补货时才引入锁。" />
<Predict no="32-2" q="一个 1 字节、不含指针的对象，最可能走哪条路径？" :options='["大对象直达 mheap","按 8 字节 size class 单独分配","合并进 tiny allocator 的 16 字节 block","分配在栈上永不进堆"]' answer="2" output="由 tiny allocator 合并分配，平均占用远小于 8 字节" explain="小于 16 字节且 noscan 的对象由 mcache 的 tiny、tinyoffset 合并到同一个 block，专门消除小对象碎片。" />

## 一页纸速记

<KnowledgeCard :points='["小对象按约 68 个 size class 向上取整，用内部碎片换无锁分配","mcache 每 P 独占、mcentral 按档加锁补货、mheap 管页与 arena","tiny 合并小于 16 字节的无指针对象，大对象超约 32 KB 直达 mheap","spanclass 的 noscan 位决定 GC 是否扫描这块 span"]' :pitfalls='["把 MemStats 差值当精确分配公式","忽视 32 KB 的分界与指针密度带来的扫描成本"]' summary="分配快的关键是把热路径做成每 P 无锁的本地缓存，冷路径才逐级上溯。" />

## 小结与下一讲

- 分档取整是速度对空间的取舍，档位表可在 `runtime/sizeclasses.go` 查到。
- 三级缓存把锁竞争收敛到 mcentral，tiny 与大对象是两条专门优化路径。
- 一切结论都可用 `go build -gcflags=-m` 与 `GODEBUG=gctrace=1` 复现。

分配来的内存由谁回收、何时回收？[下一讲：垃圾回收：三色标记、屏障与 GOGC](/core/gc)
