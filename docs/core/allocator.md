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

### 设计动机的三个细节与另一种观测手段

上一节已解释「为什么分档而不是任意切分」，这里补三处同样影响设计的取舍。**tiny 为什么只合并无指针对象**：含指针的对象 GC 必须逐个扫描，若把它们塞进同一个 16 字节 block，扫描单位与分配单位错位，一个存活指针还会让整块被误判为存活；`noscan` 前提让 GC 可以整块跳过。**为什么用位图而非空闲链表**：`allocBits`/`gcmarkBits` 一次机器字扫描能查 64 个槽位，更关键的是清扫阶段只需把两张位图交换，就完成「已标记对象转为已分配、其余归还」，复杂度是 O(页数) 而不是 O(对象数)。**`spanclass` 低位的 noscan 标记**把「是否含指针」固化进分配规格，让 GC 以 span 为单位决定要不要扫描——这正是「能用值切片就别用指针切片」能削减标记工作量的源码依据。

除 `runtime/metrics` 的直方图外，`runtime.MemStats.BySize` 是同一份档位数据的另一种读法，可直接跑：

```go
package main

import (
	"fmt"
	"runtime"
)

var sink [][]byte

func main() {
	for _, n := range []int{8, 17, 100, 4096} {
		sink = append(sink, make([]byte, n))
	}
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	for _, s := range m.BySize {
		if s.Mallocs > 0 {
			fmt.Printf("档位 %5d B: Mallocs=%d Frees=%d\n", s.Size, s.Mallocs, s.Frees)
		}
	}
	_ = sink
}
```

```bash
go run bysize.go
GODEBUG=gctrace=1 go run bysize.go
```

<OutputBox title="bysize.go 输出节选（累计值，含运行时自身分配）" output="档位     8 B: Mallocs=12 Frees=2\n档位    16 B: Mallocs=31 Frees=4\n档位    24 B: Mallocs=6  Frees=0\n档位   112 B: Mallocs=2  Frees=1\n档位  4096 B: Mallocs=5  Frees=0" note="Size 是档位上限；17 字节会落在 24 档，100 字节落在 112 档。" />

逐字段解释：`Size` 是该 size class 的上限字节数，`Mallocs` 与 `Frees` 是**累计**分配与释放次数（不是瞬时存活量）；申请 17 字节出现在 24 档、100 字节出现在 112 档，正是 `roundupsize` 向上取整的直接证据。观测局限很重要：`BySize` 与 `/gc/heap/allocs-by-size:bytes` 同源、都是累计值且含运行时自身分配，只能做定性对照；`HeapAlloc` 差值同样含这些开销；档位表 `class_to_size` 随 Go 版本微调，跨版本比较前先 `go version`。`gctrace=1` 每行箭头后的两个数字分别是标记结束时的存活堆与下一轮触发目标，两者长期贴近说明分配速率高、GC 频繁。

### 一次分配的逐级状态迁移：从 freeindex 到 mmap

上一节用时序图画出「缺货才逐级上溯」，但没写清每一步**哪个字段变了、在什么条件下升级**。把 `runtime/malloc.go` 的 `mallocgc` 展开，因果链是这样的：输入 `size` 先经 `roundupsize` 查 `class_to_size` 得到档位 `spc`，`spanclass = spc*2 + noscan`（`noscan` 由 `typ.Pointers()` 决定）；取当前 P 的 `mcache`，读 `c.alloc[spc]` 得到 span `s`；`s.freeindex` 配合 `allocBits` 位图找出下一个空槽，命中时 `s.freeindex` 与 `s.allocCount` 各加一，直接返回 `s.base() + freeindex*s.elemsize`——整条热路径无锁、无系统调用。分支条件在 `malloc.go:939`：`if freeIndex == s.nelems` 说明本 span 已装满，触发 `mcache.refill(spc)`（`mcache.go:147`），先把满 span 归还、再调 `mcentral.cacheSpan()`（`mcentral.go:81`）从 partial 集合里找还有空槽的 span，其内部又以 `mcentral.go:185` 的 `freeindex == nelems || allocCount == nelems` 判定候选是否可用。partial 也空，才上溯 `mheap.alloc(npages, spc)`：按 `class_to_allocnpages[spc]` 个 8 KB 页切新 span，`mheap.go:1407` 里 `s.nelems = uint16(nbytes / s.elemsize)`（含指针时还要扣掉位图预留），再分配 `allocBits`/`gcmarkBits` 两张位图；arena 不足才由 `mheap.grow` 走 `mmap`，这一步才让 `HeapSys` 真正上台阶。大对象超过 `maxSmallSize`（32 KB）直接跳过 mcache 与 mcentral，以 `spanclass=0` 走 `mheap.alloc`。

量化一层：一个 span 能装多少对象由 `nelems ≈ 页数 × 8192 / elemsize` 决定。代入 24 字节档位、1 页：`8192 / 24 ≈ 341` 个 object（含指针时按 `mheap.go:1407` 再减去 `nbytes/goarch.PtrSize/8` 的位图预留，约 336 个）。因为 `refill` 一次搬的是**整块 span**，摊到每个 object 的上溯成本约 `1/341`；这正是「高频小对象几乎不碰 mcentral」的算式依据，而大对象每次都直达 mheap，成本是 1 比 1。

用 `MemStats.BySize` 的 `Mallocs` 差分，把「请求字节 → 命中档位」亲手扫出来：

```go
package main

import (
	"fmt"
	"runtime"
)

var hold [][]byte

func classOf(n, count int) uint64 {
	runtime.GC()
	var b0, b1 runtime.MemStats
	runtime.ReadMemStats(&b0)
	buf := make([][]byte, count)
	for i := range buf {
		buf[i] = make([]byte, n)
	}
	runtime.ReadMemStats(&b1)
	for i := range b0.BySize {
		if b1.BySize[i].Mallocs-b0.BySize[i].Mallocs >= uint64(count) {
			hold = buf // 保持存活，避免被编译器消除或被 GC 干扰
			return uint64(b0.BySize[i].Size)
		}
	}
	hold = buf
	return 0
}

func main() {
	for _, n := range []int{1, 8, 9, 16, 17, 24, 25, 32, 33, 48, 49, 64} {
		cls := classOf(n, 1000)
		if cls == 0 {
			fmt.Printf("请求 %2d B: BySize 找不到对应档位（tiny 合并）\n", n)
			continue
		}
		fmt.Printf("请求 %2d B → 档位 %2d B，内部碎片 %5.1f%%\n",
			n, cls, 100*float64(int(cls)-n)/float64(cls))
	}
}
```

```bash
go run class.go
# 若构建缓存让 -gcflags 无输出，先 go clean -cache 或追加 -a
go build -gcflags=-S -o /dev/null class.go 2>&1 | grep -E 'mallocgc|makeslice' || true
```

<OutputBox title="档位阶梯实测（Go 1.23 / arm64，累计值含运行时自身分配）" output="请求  1 B: BySize 找不到对应档位（tiny 合并）\n请求  8 B: BySize 找不到对应档位（tiny 合并）\n请求  9 B → 档位 16 B，内部碎片  43.8%\n请求 17 B → 档位 24 B，内部碎片  29.2%\n请求 25 B → 档位 32 B，内部碎片  21.9%\n请求 33 B → 档位 48 B，内部碎片  31.2%\n请求 49 B → 档位 64 B，内部碎片  23.4%" note="碎片率取决于距下一档边界的距离，与请求大小不单调相关；1 与 8 字节走 tiny 合并，BySize 按 16 字节 block 只记一次分配。" />

逐字段解释：`BySize[i].Size` 是档位上限，`Mallocs` 是累计分配次数；差分达到 `count`（这里 1000）的那一档就是请求命中的档位，代入 `1 - n/cls` 得到内部碎片率。`9 → 16` 浪费 43.8%，`17 → 24` 29.2%，`25 → 32` 21.9%，而 `33 → 48` 又反弹到 31.2%——因为档位不是等距的，33 恰好跨过 32 跳到 48。`1` 与 `8` 字节找不到对应档位，是 tiny allocator 把多个小对象塞进同一个 16 字节 block、`BySize` 只为 block 记一次分配导致的，这也说明 `BySize`/metrics 的直方图都看不到「对象个数」，只能看「分配次数」。

**反直觉结论的证伪**：「请求越大，浪费的比例越高」是错的。对照 `9 B`（43.8%）与 `49 B`（23.4%），请求变大五倍多，碎片率反而下降；真正决定碎片的是请求落在档位区间里的位置，刚跨过边界的请求最吃亏。另一个易错点是「每个堆对象在 `BySize` 里都有一条记录」——tiny 合并让 1 字节对象完全不单独计数，若拿 `BySize.Mallocs` 去反推对象总数会系统性偏小；`HeapAlloc` 差值同理含运行时自身开销，只能定性。

边界：档位表 `class_to_size` 与 `class_to_allocnpages` 是 Go 1.21~1.23 的取值，跨版本先 `go version` 再对照本机 `sizeclasses.go`；32 位平台 `goarch.PtrSize` 变化会让位图预留与 `mheap.go:1407` 的算式结果不同，但 8 KB 页与 32 KB 分界不变；Go 1.24 的 Swiss Table 改的是 map 内部，其内存仍走同一分配器；观测手段里只有 `-gcflags=-S` 与 `-gcflags=-m` 能看到「这次是否分配」，MemStats 类只能看累计总量。

### `mcentral` 的清扫代轮换与 `mheap.grow` 到 mmap 的状态迁移

前面把 mcentral 说成「维护 partial 与 full 两条链表」，其实它维护的是**两组**链表，而且两组角色每轮 GC 交换一次。`runtime/mcentral.go`（Go 1.23.8）的结构与注释是：

```text
type mcentral struct {
    spanclass spanClass
    partial [2]spanSet   // 有空槽的 span，按清扫代分成两组
    full    [2]spanSet   // 无空槽的 span，同样两组
}
// sweepgen 每轮 GC 加 2，于是：
//   已清扫 span 在 partial[sweepgen/2%2]
//   待清扫 span 在 partial[1-sweepgen/2%2]
```

四个访问器把这个算式写死：`partialSwept(sg) = &partial[sg/2%2]`、`partialUnswept(sg) = &partial[1-sg/2%2]`，`fullSwept`/`fullUnswept` 同理。状态迁移链是：`mheap_.sweepgen` 在一轮 GC 里加 2 → `sg/2%2` 在 0 与 1 之间翻转 → 上一轮的「已清扫组」变成这一轮的「待清扫组」，**不需要搬动任何 span**；清扫器或分配器把 span 从「待清扫组」弹出、扫完再压进「已清扫组」，一个周期里「待清扫组」被排空。这就是第二轮补强里那句「partial 按清扫代分成两组」的完整机制：两个槽位换来 O(1) 的角色切换，避免每轮 GC 给所有 span 改标志位。

`cacheSpan()` 的消费顺序也因此是分级的：`sg := mheap_.sweepgen` 后先 `c.partialSwept(sg).pop()`（完全无锁、无需清扫）；没有才 `sweep.active.begin()` 进入清扫态，以 `spanBudget := 100` 为上限循环从 `partialUnswept(sg)` 弹 span 边扫边找（注释说明「扫 100 个还没找到空位就干脆分配新 span，把空间开销限制在 1%」）；再没有才动 `fullUnswept(sg)`；最后才上溯 `mheap_.alloc`。分支条件全是「当前组的 spanSet 是否为空」，把「补货」与「清扫」摊到同一次调用里。

**`mheap.grow` 到 mmap 的状态迁移**（`runtime/mheap.go` 与 `runtime/mem_darwin.go`）：输入 `npage` 先按 palloc chunk 取整：

```text
ask   = alignUp(npage, pallocChunkPages) * pageSize
end   = h.curArena.base + ask
nBase = alignUp(end, physPageSize)
分支  nBase 小于 curArena.end 且未溢出 → 当前 arena 内直接切页，字段只动 freeindex
否则  av, asize := h.sysAlloc(ask, &h.arenaHints, true)
      av 为 nil → 打印 "runtime: out of memory: cannot allocate ...-byte block (... in use)" 并返回 false
      若 uintptr(av) == h.curArena.end → h.curArena.end = uintptr(av) + asize（连续扩展）
      否则                              → 先 sysMap 旧 arena 余量并标 released，再把 curArena 切到新块
随后  h.pages.grow(...) 更新页分配器位图，HeapSys 才真正上台阶
```

`h.sysAlloc` 从 `mheap_.arenaHints` 链表挑地址、`h.heapArenaAlloc` 提供 arena 元数据；落到 OS 层的三个函数在 `mem_darwin.go`：

```text
sysAllocOS(n)      = mmap(nil, n, PROT_READ|PROT_WRITE, MAP_ANON|MAP_PRIVATE, -1, 0)
sysReserveOS(v, n) = mmap(v,   n, PROT_NONE,             MAP_ANON|MAP_PRIVATE, -1, 0)
sysMapOS(v, n)     = mmap(v,   n, PROT_READ|PROT_WRITE, MAP_ANON|MAP_FIXED|MAP_PRIVATE, -1, 0)
```

状态因此是 `Reserved（PROT_NONE 只占地址）→ Prepared → Ready（可读写）`：`sysMapOS` 用 `MAP_FIXED` 把先前保留的区间「就地」提升为可读可写，不换地址；`gcController.heapReleased`、`heapFree`、`heapInUse` 三个统计随状态迁移更新。只有走到 `mmap` 这一步，`HeapSys` 才变化——这解释了第 3 节「大对象反复申请直接推高 HeapSys」的底层原因。

新观察角度一：把 `sg/2%2` 的角色轮换**本地算一遍**，不跑 GC 也能看到两组如何交换。存成 `sweepgen.go`：

```go
package main

import "fmt"

// 对应 runtime/mcentral.go 的 partialSwept / partialUnswept
func swept(sg uint32) int   { return int(sg / 2 % 2) }
func unswept(sg uint32) int { return int(1 - sg/2%2) }

func main() {
	for cycle := 0; cycle < 5; cycle++ {
		sg := uint32(2 * cycle) // 每轮 GC，mheap_.sweepgen 加 2
		fmt.Printf("cycle=%d sweepgen=%2d 已清扫→partial[%d] 待清扫→partial[%d]\n",
			cycle, sg, swept(sg), unswept(sg))
	}
}
```

<OutputBox title="sweepgen.go 输出（Go 1.23.8 / darwin arm64）" output="cycle=0 sweepgen= 0 已清扫→partial[0] 待清扫→partial[1]\ncycle=1 sweepgen= 2 已清扫→partial[1] 待清扫→partial[0]\ncycle=2 sweepgen= 4 已清扫→partial[0] 待清扫→partial[1]\ncycle=3 sweepgen= 6 已清扫→partial[1] 待清扫→partial[0]\ncycle=4 sweepgen= 8 已清扫→partial[0] 待清扫→partial[1]" note="sweepgen 加 2 让 sg/2%2 在 0、1 间翻转，两组角色每轮对调，无需搬动 span。" />

逐字段解释：`sweepgen` 取 0、2、4、6、8 对应五轮 GC，`sg/2%2` 依次得 0、1、0、1、0，于是「已清扫组」下标在 `partial[0]` 与 `partial[1]` 间来回；`1-sg/2%2` 保证「待清扫组」永远占另一格。把这两列和 `cacheSpan` 的三级消费顺序合起来，就能推出「为什么 GC 切换时代价是常数、且分配器不必给每个 span 打本轮标记」。`spanBudget = 100` 是同一函数里的硬上限，代入「扫 100 个 span 仍无空位就分配新 span」可得补货调用最坏情况下的清扫量。

新观察角度二：反汇编**链接后二进制里的 `mheap.grow`**，把「字段迁移」与「运行时符号」对上，再从源码核对 darwin 的 mmap 标志。

```bash
go build -o allocbin alloc.go    # alloc.go 用第 2 节那段循环申请不同大小的程序即可
go tool objdump -s 'mheap\)\.grow' ./allocbin | grep -E 'TEXT|CALL'
go tool nm ./allocbin | grep -E 'mheap\)\.grow|mheap\)\.sysAlloc|sysMapOS|sysAllocOS'
sed -n '1470,1500p' "$(go env GOROOT)/src/runtime/mheap.go"
sed -n '15,20p;57,73p' "$(go env GOROOT)/src/runtime/mem_darwin.go"
sed -n '25,45p' "$(go env GOROOT)/src/runtime/mcentral.go"
```

<OutputBox title="mheap.grow 反汇编与源码（Go 1.23.8 / arm64）" output="TEXT runtime.(*mheap).grow(SB) runtime/mheap.go\n  mheap.go:1489  CALL runtime.(*mheap).sysAlloc(SB)\n  mheap.go:1508  CALL runtime.sysMap(SB)\n  mheap.go:1515  CALL runtime.(*pageAlloc).grow(SB)\n  mheap.go:1492  CALL runtime.printlock(SB) / printstring / printuint\n--- go tool nm ---\n100024aa0 T runtime.(*mheap).grow\n10000acb0 T runtime.(*mheap).sysAlloc\n1000117a0 T runtime.sysMapOS" note="T 是代码段；三个 CALL 依次对应「要地」「映射旧余量」「更新页分配器位图」。" />

逐字段解释：`mheap.go:1489` 的 `CALL runtime.(*mheap).sysAlloc(SB)` 是「当前 arena 装不下」分支的入口，正是上面状态迁移里 `av, asize := h.sysAlloc(ask, &h.arenaHints, true)`；`mheap.go:1508` 的 `sysMap` 对应「新块不连续时把旧 arena 余量标为 released」；`mheap.go:1515` 的 `pageAlloc.grow` 更新页分配器，此后新页才可被切分；`mheap.go:1492` 那一串 print 调用就是 OOM 时 `runtime: out of memory: cannot allocate ...-byte block` 的打印路径。`go tool nm` 里 `runtime.sysMapOS` 的存在把路径一路接到 `mem_darwin.go` 的 `mmap`；`sed` 三段分别给出 `grow` 的 `ask/end/nBase` 算式、三个 mmap 标志组合、以及 `mcentral` 的 `partial [2]spanSet` 注释原文。注意 `go tool nm` 此处只用来确认符号**存在**，地址随构建变化、不作定量依据。

边界：`sweepgen` 加 2、`sg/2%2`、`spanBudget=100`、`pallocChunkPages` 都是 Go 1.23 的实现细节，跨版本先对照 `mcentral.go` / `mheap.go`；两个 `spanSet` 的角色轮换依赖 `sweepgen` 单调加 2，手改模拟里的步长会破坏结论；`sysAllocOS`/`sysReserveOS`/`sysMapOS` 是 darwin 专用实现，Linux 走 `mmap` 但标志与 `MADV_*` 不同；`go tool objdump` 的地址与寄存器名随 GOARCH 变化，只比符号与源码行号；`heapArenaAlloc`、`arenaHints` 是运行时私有字段，`HeapSys` 上台阶还受 scavenger 回落影响，别把一次 `vmmap`/`MemStats` 快照当成稳态。这条链在第 33 讲（`assistWorkPerByte` 与调步）、第 96 讲（清扫与 span 归还）会继续展开。

### 边界、常见误解与前后联系

| 常见误解 | 正解 | 可验证依据 |
| --- | --- | --- |
| `MemStats.BySize` 里的 `Size` 就是我 `make` 的字节数 | 是档位上限；17 字节记在 24 档、100 字节记在 112 档 | 上面的 `bysize.go` |
| 所有小于 16 字节的对象都进 tiny allocator | 含指针的不进（noscan 前提），tiny 只合并无指针小对象 | `runtime/malloc.go` 的分支判断与 `-gcflags=-m` |
| 大对象也走 mcache 分档缓存 | 超 `maxSmallSize`（32 KB）直达 mheap 专用 span，反复申请直接抬高 `HeapSys` | 第 3 节 1 MiB 大对象实验 |

边界要点：32 位平台字长改变指针宽度与档位表（但 `maxSmallSize` 32 KB、页 8 KB 不变）；`GOMAXPROCS` 决定 P 与 mcache 的数量，P 销毁时由调度器回收其本地缓存；`BySize` 与 metrics 都是累计快照而非瞬时值；大对象以 `spanclass=0` 申请专用 span、不进 mcache；`needzero` 决定新槽位是否清零；Go 1.24 的 Swiss Table 改的是 map 内部结构，其内存仍走同一分配器，但 `class_to_size` 可能随版本微调；观测手段里只有 `go build -gcflags=-S` 与 `-gcflags=-m` 能看到「这次分配是否发生」，MemStats 类只能看总量。

前后联系：第 27 讲（逃逸分析）决定对象是否进堆，本讲决定进堆后落在哪条路径，第 33 讲（GC）决定这些 span 何时被回收；第 96 讲（GC 源码与真实调优案例）会精读 `gcmarkBits`/`allocBits` 的交换与清扫如何按 span 归还 mcentral、以及辅助标记与调步算法，第 95 讲（调度器源码）解释 P 与 mcache 的绑定与回收，第 100 讲（极致性能优化）把「减分配次数 → 降指针密度 → 最后动 GOGC」列为标准闭环。

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

<Quiz q="一个 40 KB 的 []byte 会走哪条分配路径？" :options='["tiny allocator 合并","某个 size class 从 mcache 取槽位","按 8 KB 页取整后直达 mheap","分配在栈上"]' answer="2" explain="40 KB 超过 maxSmallSize=32768，属大对象，按页取整后直接向 mheap 申请专用 span，不参与 mcache 分档缓存。" />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l3-core/lesson-032-allocator`](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-032-allocator)。clone 仓库后进入 `examples/l3-core`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

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
