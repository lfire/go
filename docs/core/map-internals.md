---
title: 第 29 讲 · map 底层：hmap、桶与渐进扩容
description: 理解哈希表的实现与性能特征：hmap/bmap 结构、哈希与桶定位、溢出桶、等量/翻倍扩容、迁移与遍历
---

<StageHeader stage="L3 · 核心机制" level="★★★★☆" time="45 分钟" goal="理解哈希表的实现与性能特征" prereq="第 16 讲 · map：哈希、遍历与 Set 技巧" />

# 第 29 讲 · map 底层：hmap、桶与渐进扩容

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

<OutputBox title="运行结果（顺序每次都可能不同）" output="k3=3 k1=1 k4=4 k0=0 k2=2\nk0=0 k2=2 k3=3 k1=1 k4=4\nk4=4 k0=0 k1=1 k2=2 k3=3" />

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

<Callout type="warn" title="unsafe 镜像的风险边界" text="用镜像结构读 count / B 只为教学：它依赖 runtime 私有布局，换一个 Go 版本就可能错位甚至崩溃。生产代码永远不要这么干，这正是第 37 讲 unsafe 主题要展开的边界。此外，并发读写 map 会直接 fatal error，delete 不会缩容，元素地址也不可取。" />

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

## 动手练一练

<Exercise title="证明遍历无序" task="写一个程序：插入 8 个键，连续 range 五次并打印键顺序；再对键做一次 sort 后输出，说明为什么业务代码需要显式排序。" :hints='["把每轮的 key 收进切片再打印","sort.Strings 可以对字符串切片排序"]' solution="for r := 0; r != 5; r++ {\n    for k := range m { fmt.Print(k) }\n    fmt.Println()\n}" explain="range 的起点是随机的，多轮顺序不同；需要稳定输出时必须把 key 收集起来显式排序。" />

## 随堂检测

<Quiz no="29-1" q="关于 Go 的 map 遍历，下列说法正确的是？" :options='["顺序固定，按插入先后","顺序按 key 排序","顺序不保证，多次遍历可能不同","只在并发时才会乱序"]' answer="2" explain="运行时为每次 range 随机选择起始桶与起始槽，渐进扩容期间还要兼顾新旧桶，因此顺序不可依赖。" />

<Predict no="29-2" q="插入 20 万个键后再删掉 19.9 万个，len 与底层桶内存会怎样？" :options='["len 与桶内存都降到 1000 量级","len 降到 1000，但桶数组不会自动缩小","len 不变","map 自动缩容并触发 panic"]' answer="1" output="len 降到 1000，桶仍在，HeapAlloc 不会成比例下降" explain="delete 只清槽位，不归还桶数组；map 没有自动缩容机制。要真正释放，就重建一个新 map 把剩余键搬过去。" />

<FillBlank no="29-3" tpl="经典 map 结构里记录桶个数对数的字段是 ___，一个 bmap 桶最多放 ___ 个键值对；负载因子超过约 6.5 会触发翻倍扩容。" :answers='["B","8"]' hint="填一个字段名与一个数字" explain="B 决定桶数为 2 的 B 次方；每桶 8 个槽，装不下时挂溢出桶。" />

## 一页纸速记

<KnowledgeCard :points='["map 变量本体只是一个指针（64 位下 8 字节），赋值只拷指针","经典实现：hmap 管目录，bmap 桶每桶 8 槽，冲突挂溢出桶","扩容分翻倍（负载因子超 6.5）与等量（溢出桶过多），采用渐进搬迁","Go 1.24 起默认 Swiss Table，内部布局改变但对外语义不变"]' :pitfalls='["依赖遍历顺序写代码","并发读写 map 会触发 fatal error","用 unsafe 镜像读运行时结构，跨版本即失效","以为 delete 大量键后会自动缩容"]' summary="map 的性能特征都来自「哈希定位 + 桶 + 渐进扩容」，且实现细节随版本演进。" />

## 小结与下一讲

- `map` 变量是指针，赋值廉价；经典实现靠 `hmap` 目录 + `bmap` 桶 + `tophash` 定位，冲突挂溢出桶。
- 翻倍/等量扩容都用渐进搬迁摊薄成本，代价是遍历无序、删除不缩容。
- Go 1.24 起默认 Swiss Table，别把内部字段当成稳定契约，更别用 `unsafe` 窥探。

切片和 map 的值都是「小头 + 指向别处」，接口也是两个字的头。可为什么一个装着 nil 指针的接口却「不等于 nil」？下一讲拆开 `iface / eface`，把这个经典陷阱讲清楚。

[下一讲：interface 底层：iface / eface 与 nil 陷阱](/core/interface-internals)
