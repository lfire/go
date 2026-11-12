---
title: 第 029 讲 · map 底层：hmap、桶与渐进扩容
description: 理解哈希表的实现与性能特征：hmap/bmap 结构、哈希与桶定位、溢出桶、等量/翻倍扩容、迁移与遍历
---

<StageHeader stage="L3 · 核心机制" level="★★★★☆" time="45 分钟" goal="理解哈希表的实现与性能特征" prereq="第 016 讲 · map：哈希、遍历与 Set 技巧" />

# 第 029 讲 · map 底层：hmap、桶与渐进扩容

> 会用 `map` 只需三行代码，但要解释「为什么遍历无序」「为什么并发写直接 fatal error」「为什么删了几十万个键内存还不降」，就得进到它的内部。这一讲把哈希表拆成目录、桶和搬迁三步，并特别标出一个容易踩坑的事实：这些内部结构在 Go 1.24 被换掉了。

## 这一讲你会得到什么

<CheckList id="goal-29" title="本讲目标" :items='["知道 map 变量本体只是一个指针，赋值为何廉价","能说清 hmap / bmap / tophash / 溢出桶各自职责","解释翻倍扩容与等量扩容的触发条件及渐进搬迁","明确 Go 1.24 Swiss Table 带来的版本差异与 unsafe 边界"]' />

## 1. 先建立直觉

把经典 `map` 想成一个带索引的档案柜：`hmap` 是贴在柜门上的目录，记着「一共多少份、分成几格」；每个 `bmap` 桶是一格抽屉，固定放 8 个槽，槽前那一小段 `tophash` 是快速筛子；同格放不下时，再挂一个「溢出桶」当外兜。查一份档案，先用哈希值定位抽屉，再用 `tophash` 在 8 个槽里快速比对。

<ConceptDiagram type="map-hmap" title="hmap → bmap 桶 → tophash 槽" caption="目录管桶数组，桶内 8 个槽，冲突挂溢出桶" />

## 2. 语法与规则

先看「map 的值到底是什么」。`unsafe.Sizeof` 会告诉你，一个 map 变量只有指针大小。

```go
package main

import (
	"fmt"
	"unsafe"
)

func main() {
	var m map[string]int
	fmt.Println("map 变量大小(字节):", unsafe.Sizeof(m)) // 8：一个 *hmap
	m = make(map[string]int, 8)
	for i := 0; i < 5; i++ {
		m[fmt.Sprintf("k%d", i)] = i
	}
	// 多次遍历顺序不保证一致：起始桶与起始槽是随机的
	for r := 0; r < 3; r++ {
		for k, v := range m {
			fmt.Printf("%s=%d ", k, v)
		}
		fmt.Println()
	}
	// 下面这行若取消注释会编译失败：cannot take address of m["k0"]
	// _ = &m["k0"]
}
```

<OutputBox title="运行结果（顺序每次都可能不同）" output="map 变量大小(字节): 8\nk0=0 k1=1 k2=2 k3=3 k4=4\nk2=2 k3=3 k4=4 k0=0 k1=1\nk4=4 k0=0 k1=1 k2=2 k3=3" />

要读内部结构，可以用镜像结构 + `unsafe` 观察。注意下面这段**只在经典实现上成立**，稍后会解释为什么。

```go
package main

import (
	"fmt"
	"unsafe"
)

// hmap 是 runtime/map.go 经典结构的镜像，仅用于教学观察。
// Go 1.24 起默认换成 Swiss Table，内部布局不同，本镜像不适用。
type hmap struct {
	count      int
	flags      uint8
	B          uint8
	noverflow  uint16
	hash0      uint32
	buckets    unsafe.Pointer
	oldbuckets unsafe.Pointer
	nevacuate  uintptr
	extra      unsafe.Pointer
}

func info(m map[int]int) *hmap {
	return (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
}

func main() {
	m := make(map[int]int)
	fmt.Println("空 map 的 B:", info(m).B)
	for i := 0; i < 14; i++ {
		m[i] = i
	}
	h := info(m)
	fmt.Println("插入 14 个后 count:", h.count)
	fmt.Println("B:", h.B, " 桶数:", uintptr(1)<<h.B)
	fmt.Printf("负载因子: %.2f\n", float64(h.count)/float64(uintptr(1)<<h.B))
}
```

<LineByLine code="type hmap struct { count int; B uint8; buckets unsafe.Pointer }\nh := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))\nfmt.Println(h.count, h.B, h.buckets)" :notes='["按运行时字段顺序手写镜像结构，才能正确读出各字段","map 变量本体是 *hmap，取其地址再解引用拿到内部结构指针","打印元素数、桶数对数与桶数组指针，都是可验证的依据"]' />

## 3. 深入一步

把镜像结构里的字段与扩容规则整理成表，性能特征就有了出处。

| hmap 字段 | 含义 |
| --- | --- |
| `count` | 当前键值对数量，`len(m)` 直接读它 |
| `B` | 桶个数的对数，桶数为 2 的 B 次方 |
| `buckets` | 当前桶数组指针 |
| `oldbuckets` | 扩容期间指向旧桶数组，搬迁完才释放 |
| `hash0` | 哈希种子，让不同 map 的哈希分布不同 |
| `flags` / `noverflow` | 状态位与溢出桶计数，用于判断是否要等量扩容 |

| 扩容类型 | 触发条件 | 动作 |
| --- | --- | --- |
| 翻倍扩容 | 负载因子 `count / 桶数` 超过约 6.5 | 桶数翻倍，重新分散键 |
| 等量扩容 | 溢出桶过多、但元素不多（删除后碎片化） | 桶数不变，重新排布以收紧存储 |
| 渐进搬迁 | 以上任一 | 不一次搬完，随后续写操作逐桶疏散 |

<Term word="负载因子（load factor）" text="元素数与桶数的比值。经典 Go map 的阈值约 6.5，超过就意味着冲突概率偏高，需要翻倍扩容来摊薄。" />

<FlowTimeline :steps='[{"title":"插入触发扩容","desc":"负载因子超过 6.5 或溢出桶过多，分配新桶数组","tone":"blue"},{"title":"标记渐进搬迁","desc":"oldbuckets 指向旧桶，进入搬迁状态","tone":"orange"},{"title":"每次写顺带搬一桶","desc":"赋值或删除时，把用到的旧桶元素迁到新桶","tone":"green"},{"title":"遍历兼顾新旧","desc":"range 同时查两个桶数组，保证不丢不重","tone":"blue"},{"title":"疏散完成","desc":"释放 oldbuckets，扩容结束","tone":"green"}]' direction="vertical" />

渐进搬迁解释了「为什么扩容不会卡住一次请求」：成本被摊到后续每次读写上，代价是扩容期间一次查找可能要查新旧两个桶。另一个可直接验证的特征是**删除不缩容**：

```go
package main

import (
	"fmt"
	"runtime"
)

func heapKB() uint64 {
	var s runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&s)
	return s.HeapAlloc / 1024
}

func main() {
	m := make(map[int]int)
	for i := 0; i < 200000; i++ {
		m[i] = i
	}
	fmt.Println("满载 len:", len(m), " HeapAlloc KB:", heapKB())
	for i := 0; i < 199000; i++ {
		delete(m, i)
	}
	fmt.Println("删到 1000 项 len:", len(m), " HeapAlloc KB:", heapKB())
}
```

这里要郑重标出**版本差异**：本讲描述的 `hmap / bmap / 溢出桶 / 负载因子 6.5` 是 Go 1.23 及之前的经典实现。**Go 1.24 起，内置 map 默认改用 Swiss Table**，用连续存储的 `ctrl / keys / values` 分组和扩展哈希替代了桶 + 溢出桶结构，性能更好、内存更省。对外可观察语义（无序、并发不安全、不能取元素地址、增长方式）保持不变，但内部字段完全不同——所以上面那段 `unsafe` 镜像在新版本上读出的 `B` 没有意义。想复现本讲实验，可以临时回退：

```bash
go version
# 回退到经典实现，复现 hmap / bmap 布局
GOEXPERIMENT=noswissmap go run main.go
```

<Callout type="warn" title="unsafe 镜像的风险边界" text="用镜像结构读 count / B 只为教学：它依赖 runtime 私有布局，换一个 Go 版本就可能错位甚至崩溃。生产代码永远不要这么干，这正是第 037 讲 unsafe 主题要展开的边界。此外，并发读写 map 会直接 fatal error，delete 不会缩容，元素地址也不可取。" />

### 推导链：从 runtime/map.go 推到每一个可观察行为

以下符号以 Go 1.23 及之前的经典实现为准，源文件是 `runtime/map.go`：`hmap` 的 `count`、`flags`、`B`、`noverflow`、`hash0`、`buckets`、`oldbuckets`、`nevacuate`、`extra` 记目录；`bmap` 以 `tophash [bucketCnt]uint8` 开头（`bucketCnt = 8`），后面跟 8 个键、8 个值与一个 `overflow *bmap`；常量 `loadFactorNum/loadFactorDen` 给出约 6.5 的负载阈值。把「结构」推到「现象」的四条链是：

- **遍历无序**：`mapiterinit` 用 `fastrand` 随机选起始桶 `r` 与起始槽 `off`，`mapiternext` 从那里绕一圈；若 `oldbuckets` 非空（渐进搬迁中），还要兼顾新旧两个桶数组。随机起点加迁移状态，共同决定了「多轮 range 顺序不同」。
- **并发写 fatal**：`mapassign` 与 `mapdelete` 开头检查 `h.flags&hashWriting`，置位则直接 `fatal("concurrent map writes")`。这是运行时主动检测，与 `-race` 的数据竞争报告是两回事。
- **元素不可取址**：`mapassign` 返回的槽位指针只在本次调用内有效，`growWork`/`evacuate` 会在后续写里把元素搬到新桶，地址随时会变，语言因此直接禁止 `&m[k]`。
- **delete 不缩容、扩容不卡顿**：`mapdelete` 只把 `tophash` 置为 `emptyOne` 并 `count--`，不归还 `buckets`；`hashGrow` 只分配新桶、把 `oldbuckets` 指向旧的并置 `nevacuate = 0`，真正搬迁由后续每次 `mapassign`/`mapdelete` 里的 `growWork` 顺带做一两个桶。

把下面四个操作存成 `map.go`，用汇编输出验证「源码语法确实被降级成这些运行时函数」：

```go
package main

import "fmt"

//go:noinline
func ops(m map[int]int, k, v int) int {
	m[k] = v
	x := m[k]
	delete(m, k)
	n := 0
	for range m {
		n++
	}
	return x + n
}

func main() {
	m := make(map[int]int)
	fmt.Println(ops(m, 1, 2))
}
```

```bash
go build -gcflags=-S -o /dev/null map.go 2>&1 | grep -E 'makemap|mapassign|mapaccess|mapdelete|mapiter'
# 若因构建缓存没有输出，先 go clean -cache 或追加 -a
```

<OutputBox title="go build -gcflags=-S 片段（arm64 / Go 1.23 实测）" output="0x0040 00064 CALL runtime.mapassign_fast64(SB)\n0x005c 00092 CALL runtime.mapaccess1_fast64(SB)\n0x0078 00120 CALL runtime.mapdelete_fast64(SB)\n0x00a4 00164 CALL runtime.mapiterinit(SB)\n0x00b8 00184 CALL runtime.mapiternext(SB)" note="四列依次是地址、偏移、指令、符号；_fast64 是按键类型选出的特化版本，amd64 下地址与寄存器名不同。" />

逐字段解释：`m[k] = v`、`x := m[k]`、`delete(m, k)` 分别降级为 `mapassign_fast64`、`mapaccess1_fast64`、`mapdelete_fast64`，`mapiterinit` 与 `mapiternext` 则成对出现在 `for range m` 里——说明 `range` 是运行时迭代器而非简单数组扫描，也是「随机起点」能生效的前提；`_fast64` 后缀由键类型与平台字长决定（32 位或 `map[string]int` 会换成别的后缀，正是版本与平台差异的一处证据）。注意 `make(map[int]int)` 在较新版本已被内联、未必出现独立的 `makemap_small` 调用，别把「某个符号没出现」直接当成「没发生分配」。另一个可直接复现的边界实验是 `nil` map：`len(nilm)`、读、`delete`、`range` 都安全，唯独 `nilm[1] = 1` 触发 `panic: assignment to entry in nil map`。

**为什么这样设计**：一次性 rehash 在大 map 上会产生肉眼可见的停顿，与低延迟目标冲突；渐进搬迁把成本摊到后续每次写，代价是扩容期间一次查找可能要查新旧两个桶、遍历要兼顾两边。桶内固定 8 槽加 1 字节 `tophash`，让查找先在一个缓存行内比完 8 个哈希前缀、命中才比完整键，CPU 缓存友好；负载因子取 6.5 是「溢出桶概率」与「内存利用率」的折中——取 1.0 会浪费大量空槽，取接近 8 会让冲突链过长。替代方案是开放寻址（线性探测），Go 1.24 的 Swiss Table 证明它吞吐更优，但要重写整个 runtime 且维持对外语义与渐进扩容不变，过渡成本极高，所以到 1.24 才默认启用。这也说明「桶加溢出桶」不是唯一解，而是特定约束下的工程取舍。

### make 的 hint 如何被换算成 B：一条可代入的公式

上一节用 `unsafe` 镜像读出了插入 14 个键后的 `B`，但没说 `make(map[K]V, hint)` 里的 hint 究竟怎么变成 B。`runtime/map.go` 的 `makemap` 把 hint 交给 `overLoadFactor(hint, B)` 反复试探：`B` 从 0 起，只要 `hint > bucketCnt`（8）且 `hint > loadFactorNum*(1<<B)/loadFactorDen`（常量 `13/2`，即约 `6.5×2^B`）就 `B++`，直到条件不成立；随后按 `bucketShift(B)` 分配 `1<<B` 个桶，写进 `h.B`，`h.count` 置 0，`h.buckets` 指向新桶数组。分支条件是 `hint <= 8` 时 B 直接保持 0。可观察输出就是 `h.B` 与桶数。

用镜像结构把 hint 到 B 的整条换算扫一遍：

```go
package main

import (
	"fmt"
	"unsafe"
)

// hmap 是 runtime/map.go 经典结构的镜像，仅用于教学观察
// Go 1.24 起默认换成 Swiss Table，本镜像不适用
type hmap struct {
	count      int
	flags      uint8
	B          uint8
	noverflow  uint16
	hash0      uint32
	buckets    unsafe.Pointer
	oldbuckets unsafe.Pointer
	nevacuate  uintptr
	extra      unsafe.Pointer
}

func fieldB(m map[int]int) uint8 {
	return (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m))).B
}

func main() {
	for _, hint := range []int{0, 8, 9, 13, 14, 52, 53, 10000} {
		b := fieldB(make(map[int]int, hint))
		fmt.Printf("hint %5d → B=%2d 桶数=%d\n", hint, b, 1<<b)
	}
}
```

```bash
go version
go run hint.go
# Go 1.24 及以上若默认 Swiss Table，用下面的开关复现经典布局
GOEXPERIMENT=noswissmap go run hint.go
```

<OutputBox title="hint → B 实测（Go 1.23 / arm64，经典实现）" output="hint     0 → B= 0 桶数=1\nhint     8 → B= 0 桶数=1\nhint     9 → B= 1 桶数=2\nhint    13 → B= 1 桶数=2\nhint    14 → B= 2 桶数=4\nhint    52 → B= 3 桶数=8\nhint    53 → B= 4 桶数=16\nhint 10000 → B=11 桶数=2048" note="每个档位的临界值就是 6.5 乘 2 的 B 次方，跨过即进一档。" />

逐字段解释：把公式写成 `B = max(0, ceil(log2(hint/6.5)))`（hint 大于 8 时），对照实测逐项代入。`9` 与 `13` 都落在 B=1：`6.5×2 = 13`，13 恰好不超过阈值；`14 > 13` 立刻进 B=2。`52 = 6.5×8` 不超阈值故 B=3，`53 > 52` 进 B=4。`hint=10000` 时 `6.5×1024 = 6656 < 10000`、`6.5×2048 = 13312 ≥ 10000`，故 B=11、2048 个桶。`hint` 为 0 或 8 时 B=0，只给 1 个桶——`hint` 是「期望元素数」而非「桶数」，除以 6.5 是因为每桶平均允许装约 6.5 个元素，这正是上一节「负载因子超过 6.5 触发翻倍扩容」的反向计算。

**反直觉结论的证伪**：「`make(map[K]V, n)` 就是预分配好能装 n 个元素的桶」是错的。n=13 只给 2 个桶、n=8 只给 1 个桶，桶数由 `hint/6.5` 反推，而不是 1 比 1；给 hint 的意义是让 `makemap` 一次把 B 定到位、省掉插入过程中的多次翻倍扩容与渐进搬迁（第 4 节 `AllocsPerRun` 量化过），不是「预留 n 个槽」。另一个易错点是「hint 越大越好」：B 每加一，桶数翻倍，`make(map[int]int, 10000)` 一上来就占 2048 个桶的内存，与实际只存少量键相比是浪费。

边界：`loadFactorNum/loadFactorDen = 13/2`、`bucketCnt = 8` 是 Go 1.23 经典实现的常量，Go 1.24 起默认 Swiss Table 没有 B 字段、`GOEXPERIMENT=noswissmap` 仅用于复现且不是长期契约；`B` 还受最大 B 限制，极大 hint 不会无限增长；`unsafe` 镜像读私有字段跨版本即错位甚至崩溃；32 位平台桶的内存布局不同但公式不变；`makemap_small` 在 hint 很小时可能被内联，别把「没看到调用」当成「没分配」。

### Go 1.24 Swiss Table：ctrl 分组、扩展哈希与一条可算的探测序列

本讲前面已标注「Go 1.24 起默认换成 Swiss Table」，但只说了「内部结构完全不同」。这一节把它推到字段级。前提必须写清：**本机是 Go 1.23.8，以下符号来自 Go 1.24.0**，可用 `GOTOOLCHAIN=go1.24.0 go version` 拉到真实工具链后逐一核对；1.23 的 `hmap / B / oldbuckets / 负载因子 6.5` 在 1.24 里不再存在。源码落点从 `runtime/map.go` 迁到 `internal/runtime/maps/`（`map.go`、`group.go`、`table.go`），`runtime/map_swiss.go` 只留一层薄包装：`makemap` 调 `maps.NewMap`，`mapassign`/`mapaccess1` 用 `//go:linkname` 指到 `internal/runtime/maps`。

**哈希被切成两半**（`internal/runtime/maps/map.go`）：

```text
h1(h) = h >> 7      // 高 57 位：选 table、选 group
h2(h) = h & 0x7f    // 低 7 位：写进控制字节
```

**ctrl 分组**（`group.go`）：`abi.SwissMapGroupSlots = 8`（`SwissMapGroupSlotsBits = 3`），一个 group 是 8 个槽加一个 `ctrlGroup uint64` 控制字，每槽 1 字节控制位。编码只有三种：

```text
empty   = 0b10000000   (ctrlEmpty)
deleted = 0b11111110   (ctrlDeleted)
full    = 0 hhhhhhh    (最高位 0，低 7 位就是 h2)
```

最高位把「空/删」与「占用」分开，于是 `ctrlGroup.matchH2(h2)` 能用一条 64 位运算**并行比对 8 个槽**（`v := g ^ (bitsetLSB * h2)`；`return ((v - bitsetLSB) &^ v) & bitsetMSB`），命中集合用 `bitset.first() = TrailingZeros64(b) >> 3` 取第一个候选；amd64 上这段被 SIMD intrinsic 替换。因为只用了 7 位哈希，单槽假阳性约 `1/128 ≈ 0.7%`，所以每个命中后仍要比完整 key。经典 `bmap` 的 `tophash [8]uint8` 也是 8 个前缀，但它是逐字节比较；Swiss Table 把它压成一次机器字/一条 SIMD 指令，这是吞吐差异的第一来源。

**探测序列**（`table.go`）：`probeSeq` 用三角形（二次）探测 `p(i) := (i² + i)/2 + h1 (mod mask+1)`，`makeProbeSeq(h1, mask)` 令 `offset = h1 & mask`、`index = 0`，`next()` 执行 `index++；offset = (offset + index) & mask`。当 group 数是 2 的幂时，`(i²+i)/2` 在 `Z/(2^m)` 上是双射，一个长度 `mask+1` 的周期恰好访问每个 group 一次；探测在「遇到含空槽的 group」时终止。删除因此不能随便把槽标 empty：若该 group 已满，必须标 `ctrlDeleted` 墓碑，否则后续探测会提前停；插入优先复用墓碑。经典 hmap 用溢出桶链表承接冲突，Swiss Table 用组内并行加组间二次探测，不再有溢出桶。

**负载与增长不是一回事**（`table.go`）：`maxAvgGroupLoad = 7`（7/8 = 0.875，与 Abseil 一致，但 Abseil 一组 16 槽），`resetGrowthLeft` 的算式是：

```text
capacity 小于等于 8  → growthLeft = capacity - 1（单 group 必须留 1 个空槽终止探测）
capacity 大于 8      → growthLeft = capacity * 7 / 8
```

代入 16/64/1024 得 14/56/896。`table` 记 `used`、`capacity`、`growthLeft`、`localDepth`、`index`，`maxTableCapacity = 1024` 是一张表分裂前的上限。

**扩展哈希取代 `oldbuckets`**（`map.go` 顶层注释）：`Map` 的 `dirPtr` 指向一个长度 `1 << globalDepth` 的目录，目录项是 `*table`，多个目录项可指向同一张表；`h1` 的高位（配合 `globalShift = 64 - globalDepth`）选目录下标，低位在表内选 group。某张表满了不是整表翻倍，而是 `table.split` 把它按 `localDepthMask(localDepth)` 拆成两张、`Map.installTableSplit` 更新目录——**渐进扩容被保留，但载体从 `oldbuckets + nevacuate` 换成了目录分裂**，`nevacuate`、`overflow` 这类字段自然消失。≤8 个键的小 map 更干脆：`dirPtr` 直接指向单个 group，`dirLen = 0`，走 `putSlotSmall` / `getWithKeySmall`，连 `table` 都不建。`runtime/map_swiss.go` 里仍留 `loadFactorNum = 7`，但已找不到 `hmap`、`B`、`oldbuckets`。

新观察角度一：**把算法搬到本地算一遍**，不依赖 1.24 运行时也能看到 ctrl 位图与探测序列。存成 `swiss.go`（公式与 1.24 源码逐行对应）：

```go
package main

import (
	"fmt"
	"math/bits"
)

const (
	ctrlEmpty   = uint8(0b10000000)
	ctrlDeleted = uint8(0b11111110)
	bitsetLSB   = uint64(0x0101010101010101)
	bitsetMSB   = uint64(0x8080808080808080)
	groupSlots  = 8
	maxLoad     = 7
)

// 对应 Go 1.24 internal/runtime/maps/map.go 的 h1 / h2
func h1(h uint64) uint64 { return h >> 7 }
func h2(h uint64) uint64 { return h & 0x7f }

// 对应 group.go 的 ctrlGroupMatchH2 可移植实现
func matchH2(g uint64, h uint64) uint64 {
	v := g ^ (bitsetLSB * h)
	return ((v - bitsetLSB) &^ v) & bitsetMSB
}

// 对应 table.go 的 probeSeq：p(i) = (i^2+i)/2 + h1 (mod mask+1)
func probe(h uint64, mask uint64, steps int) []uint64 {
	off := h & mask
	out := []uint64{off}
	for i := 1; i <= steps; i++ {
		off = (off + uint64(i)) & mask
		out = append(out, off)
	}
	return out
}

// 对应 table.go 的 resetGrowthLeft
func growthLeft(capacity int) int {
	if capacity <= groupSlots {
		return capacity - 1
	}
	return capacity * maxLoad / groupSlots
}

func main() {
	var g uint64
	ctrls := []uint8{0x2a, ctrlEmpty, 0x2a, 0x2a, 0x05, ctrlDeleted, 0x2a, ctrlEmpty}
	for i, c := range ctrls {
		g |= uint64(c) << (8 * uint(i))
	}
	m := matchH2(g, 0x2a)
	fmt.Printf("ctrlGroup=%016x  h2=0x2a  matchH2=%016x\n", g, m)
	fmt.Print("命中槽位:")
	for i := 0; i < groupSlots; i++ {
		if m&(uint64(0x80)<<(8*uint(i))) != 0 {
			fmt.Printf(" %d", i)
		}
	}
	fmt.Printf("  首个=%d\n", bits.TrailingZeros64(m)>>3)

	for _, cap := range []int{8, 16, 64, 1024} {
		fmt.Printf("capacity=%4d growthLeft=%4d 负载上限=%.3f\n",
			cap, growthLeft(cap), float64(growthLeft(cap))/float64(cap))
	}

	h := uint64(0x9e3779b97f4a7c15)
	fmt.Printf("hash=0x%016x h1=0x%013x h2=0x%02x\n", h, h1(h), h2(h))
	for _, mask := range []uint64{3, 7} {
		fmt.Printf("groups=%d 探测序列(整周期)=%v\n", mask+1, probe(h1(h), mask, int(mask)))
	}
}
```

<OutputBox title="swiss.go 输出（公式代入值，1.23/1.24 都能跑）" output="ctrlGroup=802afe052a2a802a  h2=0x2a  matchH2=0080000080800080\n命中槽位: 0 2 3 6  首个=0\ncapacity=   8 growthLeft=   7 负载上限=0.875\ncapacity=  16 growthLeft=  14 负载上限=0.875\ncapacity=  64 growthLeft=  56 负载上限=0.875\ncapacity=1024 growthLeft= 896 负载上限=0.875\nhash=0x9e3779b97f4a7c15 h1=0x13c6ef372fe94f8 h2=0x15\ngroups=4 探测序列(整周期)=[0 1 3 2]\ngroups=8 探测序列(整周期)=[0 1 3 6 2 7 5 4]" note="命中槽位恰是放入 h2=0x2a 的 0/2/3/6；两个探测序列都是 0 到 mask 的一个排列。" />

逐字段解释：`ctrlGroup=802afe052a2a802a` 是小端排布的 8 个控制字节（槽 1 与 7 是 `0x80` empty、槽 5 是 `0xfe` deleted、其余为 full）；`matchH2=0080000080800080` 里每个 `0x80` 标记一个命中字节，落在 0、2、3、6 四个槽，正是 `h2=0x2a` 的那几个 full 槽——`0x05` 与 `0xfe` 都不误报，验证了「最高位区分空/删/占用」。`首个=0` 是 `TrailingZeros64 >> 3` 的结果。`growthLeft` 四行代入 `capacity*7/8`，8 槽特例是 `8-1=7`；`0.875` 与经典实现 `13/2 ÷ 8 = 0.8125` 的差距，就是「每 8 槽允许 7 满」对「每桶平均 6.5 满」的取舍。最后两行把 `probeSeq` 的公式坐实：4 组的 `[0 1 3 2]` 与 8 组的 `[0 1 3 6 2 7 5 4]` 都是 `0..mask` 的排列，一个周期内每组合好访问一次，这正是三角形探测在 2 的幂 group 数上为双射的可观察证据。

新观察角度二：**用真实 1.24 工具链核对符号**，而不是只信文字。下面这组命令不需要改本机默认 Go：

```bash
GOTOOLCHAIN=go1.24.0 go version
GOTOOLCHAIN=go1.24.0 go build -o m24bin m24.go
go tool nm ./m24bin | grep 'internal/runtime/maps' | head -12
R24=$(GOTOOLCHAIN=go1.24.0 go env GOROOT)
grep -n 'loadFactorNum\|SwissMapGroupSlots' "$R24/src/runtime/map_swiss.go" "$R24/src/internal/abi/map_swiss.go"
grep -c 'type hmap\|oldbuckets\|nevacuate' "$R24/src/runtime/map_swiss.go"
go build -o m23bin m24.go && go tool nm ./m23bin | grep -E 'runtime\.makemap|runtime\.mapassign' | head -4
```

`m24.go` 就是第 2 节那个建 map、插 8 个键再 `len`/读的程序（用 `make(map[int]int, 8)` 即可）。

<OutputBox title="1.24 符号与 1.23 对照（darwin arm64）" output="100002a90 T internal/runtime/maps.NewMap\n100002fb0 T internal/runtime/maps.(*Map).getWithKey\n100003200 T internal/runtime/maps.(*Map).putSlotSmall\n100002d10 T internal/runtime/maps.(*Map).installTableSplit\n100004260 T internal/runtime/maps.(*table).getWithKey\n1000051d0 T internal/runtime/maps.(*table).grow\n100004ff0 T internal/runtime/maps.(*table).split\n100004060 T internal/runtime/maps.newTable\n--- 1.24 源码 ---\nruntime/map_swiss.go:18: loadFactorNum = 7\ninternal/abi/map_swiss.go:18: SwissMapGroupSlots = 8\n--- 1.23 对照 ---\n100061f40 T runtime.makemap\n100062310 T runtime.mapassign" note="T 是代码段；1.24 的目录分裂与 table.grow 都成了独立符号，1.23 仍是 makemap/mapassign。" />

逐字段解释：`internal/runtime/maps.NewMap` 是 1.24 `makemap` 的实际落点；`putSlotSmall` 对应 ≤8 键的单 group 快路径；`installTableSplit` 与 `(*table).split` 是「渐进扩容换成目录分裂」的符号证据；`newTable`/`(*table).grow`/`getWithKey` 说明查找与增长都下沉到 `table`。`loadFactorNum = 7` 与 `SwissMapGroupSlots = 8` 把 7/8 负载与 8 槽分组从注释变成可查常量；`grep -c` 对 `type hmap`/`oldbuckets`/`nevacuate` 计数为 0，证明那套字段确实被移除。1.23 对照里的 `runtime.makemap` / `runtime.mapassign` 仍是本讲前几节推导的落点——同一份 `m24.go`、两个工具链，符号表把版本差异直接摊开。

边界：以上全部是 Go 1.24.0 的实现细节，`GOTOOLCHAIN` 切换只是拉工具链、不改变本机默认 1.23.8 的结论；`internal/runtime/maps` 是 internal 包，业务代码不可导入，本讲只作源码依据；`go tool nm` 的地址随构建变化、只比符号名；`matchH2` 的假阳性率 1/128 决定了「命中后必须比完整 key」，别把 bitset 当精确匹配；`probeSeq` 的双射性依赖 group 数为 2 的幂，模拟里周期只取 `mask+1` 步；`GOEXPERIMENT=noswissmap` 在 1.24 只用于回退复现、不是长期契约；对外语义（无序、并发不安全、不能取元素地址、删除不缩容）与经典实现一致，本讲前面的结论仍然成立。下一版可继续展开的点是目录分裂时 `globalDepth` 与 `globalShift` 的联动、以及 `Iter` 如何在分裂期间保证不丢不重。

### 边界、常见误解与前后联系

| 常见误解 | 正解 | 可验证依据 |
| --- | --- | --- |
| `delete` 大量键后 map 会缩容、内存归还 | `mapdelete` 只清槽位并 `count--`，桶数组保留；要释放需新建小 map 搬剩余键 | 第 3 节的 `HeapAlloc` 实验 |
| `range` 偶尔两轮顺序相同说明有序 | `mapiterinit` 每次随机起点，扩容期还兼顾新旧桶，顺序永不保证；要稳定输出必须显式 `sort` | 第 2 节连跑三轮的输出 |
| 并发读写 map 用 `-race` 会报 data race，加锁再测就行 | `hashWriting` 位让运行时直接 fatal error，该检测与竞态检测器无关 | 多 goroutine 无锁写同一 map 立即崩 |

边界要点：`nil` map 的读、`len`、`delete`、`range` 都安全，写入 panic；迭代期间删除或新增键是否被遍历到不保证；map 的 key 必须可比较（slice、map、func 不能作键）；Go 1.24 起默认 Swiss Table，`hmap`/`bmap` 这套字段全部失效，`GOEXPERIMENT=noswissmap` 仅用于复现经典布局、不是长期契约；`unsafe.Sizeof(m)` 只给指针宽度，看不到桶内存；用 `unsafe` 镜像读私有字段跨版本即错位甚至崩溃；`map` 不是并发安全容器，多 goroutine 共享必须外部同步或换 `sync.Map`（阶段四）。

前后联系：第 016 讲（map 的使用）与第 026 讲（描述符）在这里合流；第 098 讲（编译器源码：SSA、逃逸与内联）会看 map 语法在 `cmd/compile/internal/walk` 阶段如何被降级为上述运行时调用、以及 `_fast64` 这类按类型特化是怎么选出来的，第 097 讲（channel/mutex 源码与锁竞争）给出并发 map 与 `sync.Map` 的实现依据，第 100 讲（极致性能优化）把 `make(map[K]V, n)` 预分配列为常规优化。另外第 096 讲（GC 源码）会解释 `bmap.overflow` 这类指针字段为何让 map 所在 span 必须是 scan 而不是 noscan。

## 4. 工程实践

知道扩容代价，最直接的收益就是「建 map 时给足 hint」。用 `testing.AllocsPerRun` 量化：

```go
package main

import (
	"fmt"
	"testing"
)

func noHint(n int) map[int]int {
	m := map[int]int{} // 无 hint，插入过程多次翻倍扩容
	for i := 0; i < n; i++ {
		m[i] = i
	}
	return m
}

func withHint(n int) map[int]int {
	m := make(map[int]int, n) // 一次给足容量
	for i := 0; i < n; i++ {
		m[i] = i
	}
	return m
}

func main() {
	const n = 10000
	a := testing.AllocsPerRun(20, func() { _ = noHint(n) })
	b := testing.AllocsPerRun(20, func() { _ = withHint(n) })
	fmt.Printf("无 hint 每次分配约 %.0f 次，带 hint 约 %.0f 次\n", a, b)
}
```

<Compare :panels='[{"label":"无 hint","code":"m := map[int]int{}","note":"插入过程多次翻倍扩容与搬迁"},{"label":"带 hint","code":"m := make(map[int]int, n)","note":"一次给足容量，显著减少扩容次数"}]' title="构建大 map 的两种写法" />

工程清单：能预估规模就 `make(map[K]V, n)`；任何依赖遍历顺序的逻辑都要改成显式排序；多 goroutine 共享时加锁或用并发安全的替代方案（阶段四展开）；删除大量键后若确实要释放内存，做法是新建一个小 map 把剩余键搬过去，而不是指望 `delete`；最后，别用 `unsafe` 去窥探内部字段，升级 Go 版本时这类代码是第一批坏掉的。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l3-core/lesson-029-map-internals`](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-029-map-internals)。clone 仓库后进入 `examples/l3-core`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="证明遍历无序" task="写一个程序：插入 8 个键，连续 range 五次并打印键顺序；再对键做一次 sort 后输出，说明为什么业务代码需要显式排序。" :hints='["把每轮的 key 收进切片再打印","sort.Strings 可以对字符串切片排序"]' solution="for r := 0; r != 5; r++ {\n    for k := range m { fmt.Print(k) }\n    fmt.Println()\n}" explain="range 的起点是随机的，多轮顺序不同；需要稳定输出时必须把 key 收集起来显式排序。" />

## 随堂检测

<Quiz no="029-1" q="关于 Go 的 map 遍历，下列说法正确的是？" :options='["顺序固定，按插入先后","顺序按 key 排序","顺序不保证，多次遍历可能不同","只在并发时才会乱序"]' answer="2" explain="运行时为每次 range 随机选择起始桶与起始槽，渐进扩容期间还要兼顾新旧桶，因此顺序不可依赖。" />

<Predict no="029-2" q="插入 20 万个键后再删掉 19.9 万个，len 与底层桶内存会怎样？" :options='["len 与桶内存都降到 1000 量级","len 降到 1000，但桶数组不会自动缩小","len 不变","map 自动缩容并触发 panic"]' answer="1" output="len 降到 1000，桶仍在，HeapAlloc 不会成比例下降" explain="delete 只清槽位，不归还桶数组；map 没有自动缩容机制。要真正释放，就重建一个新 map 把剩余键搬过去。" />

<FillBlank no="029-3" tpl="经典 map 结构里记录桶个数对数的字段是 ___，一个 bmap 桶最多放 ___ 个键值对；负载因子超过约 6.5 会触发翻倍扩容。" :answers='["B","8"]' hint="填一个字段名与一个数字" explain="B 决定桶数为 2 的 B 次方；每桶 8 个槽，装不下时挂溢出桶。" :explains='["hmap 存的是桶数对 2 取对数得到的 B，桶数为 2 的 B 次方；扩容只需把 B 加一，桶数便翻倍。","每个 bmap 固定 8 个槽位，配合 top hash 快速筛选；一桶装满后不立刻扩容，而是先挂溢出桶承接多出的键值。"]' />

## 一页纸速记

<KnowledgeCard :points='["map 变量本体只是一个指针（64 位下 8 字节），赋值只拷指针","经典实现：hmap 管目录，bmap 桶每桶 8 槽，冲突挂溢出桶","扩容分翻倍（负载因子超 6.5）与等量（溢出桶过多），采用渐进搬迁","Go 1.24 起默认 Swiss Table，内部布局改变但对外语义不变"]' :pitfalls='["依赖遍历顺序写代码","并发读写 map 会触发 fatal error","用 unsafe 镜像读运行时结构，跨版本即失效","以为 delete 大量键后会自动缩容"]' summary="map 的性能特征都来自「哈希定位 + 桶 + 渐进扩容」，且实现细节随版本演进。" />

## 小结与下一讲

- `map` 变量是指针，赋值廉价；经典实现靠 `hmap` 目录 + `bmap` 桶 + `tophash` 定位，冲突挂溢出桶。
- 翻倍/等量扩容都用渐进搬迁摊薄成本，代价是遍历无序、删除不缩容。
- Go 1.24 起默认 Swiss Table，别把内部字段当成稳定契约，更别用 `unsafe` 窥探。

切片和 map 的值都是「小头 + 指向别处」，接口也是两个字的头。可为什么一个装着 nil 指针的接口却「不等于 nil」？下一讲拆开 `iface / eface`，把这个经典陷阱讲清楚。

[下一讲：interface 底层：iface / eface 与 nil 陷阱](/core/interface-internals)
