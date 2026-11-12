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

## 4. 工程实践

高频路径上，分配「次数」比分配「总量」更伤：每次都要走分档、位图与可能的补货。把零散小对象合并成一次预分配，往往比逐条 new 快一个量级。

<Compare :panels='[{"label":"逐条分配","code":"out := make([]*Item, 0, n)\nfor i := 0; i != n; i++ { out = append(out, new(Item)) }","note":"n 次分配，n 个指针，GC 需逐个扫描"},{"label":"预分配连续块","code":"buf := make([]Item, n)\nfor i := range buf { buf[i].ID = i }","note":"1 次分配，值连续，可打 noscan 标记"}]' title="减少分配次数与指针密度" />

要点：优先 `make` 预分配容量；能用值切片就别用指针切片；让大对象生命周期清晰，避免误留在长命 span 里。这些判断都能用 `go build -gcflags=-m`、`go test -bench=. -benchmem` 与 `GODEBUG=gctrace=1` 量化验证，而不是凭感觉。

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
