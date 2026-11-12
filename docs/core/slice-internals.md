---
title: 第 28 讲 · 切片底层：三元组、扩容与共享陷阱
description: 彻底掌握 slice 的内存行为：SliceHeader、共享底层数组、扩容策略版本差异、append 别名陷阱与 full slice expression
---

<StageHeader stage="L3 · 核心机制" level="★★★★☆" time="45 分钟" goal="彻底掌握 slice 的内存行为" prereq="第 27 讲 · 逃逸分析与编译器决策" />

# 第 28 讲 · 切片底层：三元组、扩容与共享陷阱

> 切片是 Go 里用得最多、也最容易「以为懂了」的结构。为什么两个切片会互相影响？为什么 `append` 有时改动原数组、有时又完全无关？为什么打印出来的 `cap` 总对不上「翻倍」的说法？答案都在那个只有 24 字节的头里，以及它背后版本不断演进的扩容策略。

## 这一讲你会得到什么

<CheckList id="goal-28" title="本讲目标" :items='["能画出 SliceHeader 的三个字段并解释各自作用","能判断两个切片此刻是否共享底层数组","说清 Go 1.18 前后扩容策略的差异与 roundupsize","会用 full slice expression 与 copy 规避别名陷阱"]' />

## 1. 先建立直觉

切片变量本身不是数组，而是一张「取货单」： `Data` 记着货物（底层数组）在哪， `Len` 记着当前取了几件， `Cap` 记着从起点最多能取几件。复制取货单很便宜，但两张单子可能指向同一仓库——这就是共享。一旦要取的件数超过 `Cap`，就得换个更大的仓库、把货搬过去，此后两张单子各管各的。

<ConceptDiagram type="slice-header" title="切片三元组：Data / Len / Cap" caption="头只有三个字，数据体在别处；多个头可以指向同一个数组" />

## 2. 语法与规则

先用手写镜像结构把头「拆开」看清楚。`unsafe.Sizeof` 与指针重解释都是编译期/只读观察，不改动数据。

```go
package main

import (
	"fmt"
	"unsafe"
)

// sliceHeader 与运行时布局一致，仅用于教学观察
type sliceHeader struct {
	Data unsafe.Pointer
	Len  int
	Cap  int
}

func main() {
	s := make([]int, 3, 5)
	s[0], s[1], s[2] = 10, 20, 30
	h := (*sliceHeader)(unsafe.Pointer(&s))
	fmt.Println("切片头字节数:", unsafe.Sizeof(s)) // 24
	fmt.Println("头里的 Len / Cap:", h.Len, h.Cap) // 3 / 5
	fmt.Printf("Data 指向: %p\n", h.Data)
	fmt.Println("首元素值:", *(*int)(h.Data)) // 10
}
```

<LineByLine code="type sliceHeader struct { Data unsafe.Pointer; Len int; Cap int }\nh := (*sliceHeader)(unsafe.Pointer(&s))\nfmt.Println(h.Len, h.Cap)" :notes='["手写与运行时布局一致的头：一个数据指针加两个整数","把切片变量的地址重新解释成头，直接读三个字段","打印 Len 与 Cap，结果与 len(s)、cap(s) 完全一致"]' />

共享与「分家」发生在 `append` 是否触发扩容的分界线上：

```go
package main

import "fmt"

func main() {
	base := []int{1, 2, 3, 4}
	a := base[1:3] // len=2, cap=3，与 base 共享
	a[0] = 99      // 改动 base 也能看见
	fmt.Println("改 a 后 base:", base)

	b := append(a, 7) // cap 够用，不扩容，写在同一数组
	fmt.Println("append 未扩容 b:", b, " base:", base)

	c := append(a, 8, 9, 10) // 超出 cap，触发扩容，另起炉灶
	c[0] = 777
	fmt.Println("base:", base, " c:", c)
	fmt.Printf("a 底层 %p, c 底层 %p\n", &a[0], &c[0])
}
```

<OutputBox title="运行结果（地址每次运行不同）" output="改 a 后 base: [1 99 3 4]\nappend 未扩容 b: [99 3 7]  base: [1 99 3 7]\nbase: [1 99 3 7]  c: [777 3 8 9 10]\na 底层 0xc000010240, c 底层 0xc0000120a0" />

逐行对一遍：`a[0] = 99` 改到了 `base[1]`；`append(a, 7)` 因 `cap(a)=3` 够用，直接把 `base[3]` 写成 7；再 `append(a, 8, 9, 10)` 需要容量 5 超过 3，于是分配新数组，`c` 与 `base` 从此无关，两个 `Data` 地址也不同。下面的交互台可以一步步看这次「分家」。

<ConceptDiagram type="slice-grow" title="扩容为什么会「分家」：旧数组与新数组" />

<StepLab scene="slice-grow" />

## 3. 深入一步

扩容到底涨多少？别背口诀，跑一遍最可靠：

```go
package main

import "fmt"

func main() {
	var s []int
	prev := 0
	for i := 1; i <= 600; i++ {
		s = append(s, i)
		if c := cap(s); c != prev {
			if prev == 0 {
				fmt.Printf("len=%-4d cap=%-4d\n", len(s), c)
			} else {
				fmt.Printf("len=%-4d cap=%-4d 约 %.2f 倍\n", len(s), c, float64(c)/float64(prev))
			}
			prev = c
		}
	}
}
```

<OutputBox title="64 位 / Go 1.23 / []int 实测（版本或元素类型不同会变化）" output="len=1    cap=1\nlen=2    cap=2    约 2.00 倍\nlen=3    cap=4    约 2.00 倍\nlen=5    cap=8    约 2.00 倍\nlen=9    cap=16   约 2.00 倍\n...\nlen=129  cap=256  约 2.00 倍\nlen=257  cap=512  约 2.00 倍\nlen=513  cap=848  约 1.66 倍" />

```bash
# 先确认版本，再跑上面程序对比
go version
go run main.go
# 换成 []byte 或 32 位架构，会看到不同的 cap 序列
GOARCH=386 go run main.go
```

这里的**版本差异**必须记牢：

| Go 版本 | 扩容规则 | 阈值与说明 |
| --- | --- | --- |
| 1.18 之前 | `cap < 1024` 翻倍，否则按 1.25 倍增长 | 阈值 1024，硬切换 |
| 1.18 及之后 | `cap < 256` 翻倍，否则按 `newcap += (newcap + 3*256) / 4` 平滑过渡 | 阈值 256，增长趋近 1.25 倍 |
| 所有版本 | 目标容量再经 `roundupsize` 按元素大小向上对齐 | 实际 `cap` 常大于公式值，例如 512 之后可能得到 848 |

<Term word="roundupsize" text="内存分配器把请求字节数向上取整到预定义 size class 的过程。它让最终容量贴合分配规格，也是「公式算出的 cap」与「打印出的 cap」对不上的根本原因。" />

所以任何写死「1024 之后 1.25 倍」或「永远翻倍」的说法都过时了。此外还有一条容易忽略：若一次 `append` 需要的容量就超过旧容量的两倍，运行时直接取所需容量，而不是机械翻倍。结论只能来自本机 `go version` 加实测，不能来自博客结论。

<Callout type="warn" title="append 的三条铁律" text="一，结果一定要接住并传回给所有持有方；二，把切片传进函数后 append，不会改变调用方的 len——头是值拷贝（回看第 26 讲）；三，从大数组切一小片长期持有，会拖住整块底层数组无法回收，是常见的内存泄漏。" />

### 深入一步：growslice 源码、实验与生产观测

以下路径以 Go 1.21~1.23 的 `$GOROOT/src/runtime` 为准，跨版本函数名会变，动手前先 `go version`。切片头在反射包的公开镜像是 `reflect.SliceHeader`，字段为 `Data uintptr`、`Len int`、`Cap int`；运行时内部等价布局里 `Data` 其实是 `unsafe.Pointer`。关键区别在于：`uintptr` 只是整数，不参与 GC 存活判定，把它取出来长期保存，底层数组可能在你仍使用它时就被回收。所以别手工拼头，构造切片用 Go 1.17 起的 `unsafe.Slice` 与 Go 1.20 起的 `unsafe.SliceData`。

扩容在 `runtime/slice.go` 的 `growslice`：Go 1.18 把阈值从 1024 降为 256 并改平滑增长，Go 1.21 又把容量计算抽成 `nextslicecap`。它先按元素大小把目标容量换算成字节，再经 `runtime/msize.go` 的 `roundupsize` 对齐到 size class，最后交 `mallocgc` 分配并搬移旧元素；元素含指针时还要走写屏障辅助路径，这正是 `growslice` 里 `et.Pointers() != 0` 分支存在的原因。

三个实验可自行复现：`go build -gcflags=-m slice.go 2>&1 | grep -E 'makeslice|growslice|escapes'` 看逃逸与扩容调用，`go tool compile -S slice.go | grep growslice` 能看到 `CALL runtime.growslice`，`GODEBUG=gctrace=1 go run slice.go` 则显示频繁 append 时堆规模逐轮上台阶。生产里 pprof 中 `runtime.growslice` 进热点前列，几乎就是没预估容量的信号；`go test -bench=. -benchmem` 的 `allocs/op` 会把扩容次数直接量化。调优按代价从小到大：`make([]T, 0, n)` 一次预留、Go 1.21 `slices.Grow` 主动扩容、把临时缓冲收进更短的小函数。常见误用是把 `reflect.SliceHeader` 当公开 API 来回转换，或以为 `cap` 必等于公式值而忽略 `roundupsize`。

把源码与现象对起来更清楚：观察程序里 512 之后跳到 848，不是公式算错，而是平滑公式先给出 832 个元素、832×8=6656 字节，经 `roundupsize` 向上对齐到 6784 字节档位后再除回 8，得到 848。`growslice` 用 `memmove` 按元素大小乘旧长度搬数据，含指针时改走带写屏障的路径，返回全新的头，旧数组等引用归零后由 GC 回收，这解释了扩容后 `Data` 地址必然变化。生产里 `go build -gcflags='-m -m'` 能看到 `makeslice` 的逃逸判定，配合 `go test -benchmem` 的 `B/op`：它明显大于元素总字节时，差值就是历次扩容累计分配与档位取整之和。另一个高频误用是给 `unsafe.Slice` 传负长度或指向已释放内存的指针，二者都会在运行时崩，且绕过了编译器边界检查。

| 字段 | 类型 | 含义 | 使用时的注意 |
| --- | --- | --- | --- |
| `Data` | `uintptr` | 底层数组首元素地址 | 不参与 GC 存活判定，别当整数长期持有 |
| `Len` | `int` | 当前可见元素个数 | `append` 只改头里的 `Len`，不改调用方的头 |
| `Cap` | `int` | 从 `Data` 起可用的槽位数 | 经 `roundupsize` 对齐，常大于公式值 |

<SequenceDiagram title="append 触发扩容时的调用链" caption="从容量判断到新数组返回，每一步都对应一个可查证的运行时函数" :actors='["用户代码","append","growslice","roundupsize","mallocgc"]' :steps='[{"from":"用户代码","to":"append","label":"append(s, x)","kind":"call"},{"from":"append","to":"growslice","label":"cap 不足触发扩容","kind":"call"},{"from":"growslice","to":"growslice","label":"按 256 阈值算","kind":"self"},{"from":"growslice","to":"roundupsize","label":"对齐字节数","kind":"call"},{"from":"roundupsize","to":"mallocgc","label":"申请新 span","kind":"call"},{"from":"mallocgc","to":"growslice","label":"返回新数组","kind":"return"},{"from":"growslice","to":"append","label":"拷贝并返回新头","kind":"return"},{"from":"append","to":"用户代码","label":"交还新三元组","kind":"return"}]' />

## 4. 工程实践

`full slice expression`（三索引切片）把 `cap` 锁死，从而强制 `append` 另分配，是规避别名写的标准手法；`copy` 则能把数据搬到独立数组，彻底切断共享。顺带一提，Go 1.21 起标准库 `slices.Clip` 能把容量收缩到长度，效果等价于三索引切片，适合在处理完数据后主动切断共享。

```go
package main

import "fmt"

func main() {
	big := make([]int, 1000)
	for i := range big {
		big[i] = i
	}

	part := big[:3:3] // len=3, cap=3，append 必然另分配
	fmt.Println("part len/cap:", len(part), cap(part))
	part = append(part, 999)
	fmt.Println("big[3] 仍是:", big[3]) // 3，未被覆盖

	alias := big[:3] // cap 继承为 1000
	alias = append(alias, 888)
	fmt.Println("big[3] 变成:", big[3], " alias:", alias) // 888，被意外改写

	keep := make([]int, 3) // 想长期只留 3 个元素，用 copy 切断
	copy(keep, big[:3])
	fmt.Println("keep:", keep)
}
```

<Compare :panels='[{"label":"不带 max","code":"alias := big[:3]","note":"cap 继承到底，append 可能覆盖 big 后续元素"},{"label":"full slice expr","code":"part := big[:3:3]","note":"cap 被锁成 3，append 必然另分配"}]' title="两种切法，两种后果" />

落地建议：构造切片时尽量 `make([]T, 0, n)` 预估容量，既省扩容又少一次堆分配（第 27 讲）；函数若可能扩容，就把新切片作为返回值传回，别指望调用方「自动看见」；需要把子切片交给缓存、队列或 goroutine 长期持有时，先 `copy` 或复用三索引表达式，避免把整个大数组钉在内存里。排查线上「数据被莫名改写」时，先画一遍 `Data` 指向与 `cap`，八成是别名写在作祟。

### 用基准测试把扩容次数量化

把下面代码存成 `slice_bench_test.go` 跑基准，用包级 `sink` 承接结果，避免循环被优化掉。

```go
package main

import "testing"

var sink []int

// 不预估容量：append 反复触发 growslice，产生多次分配与旧元素拷贝
func BenchmarkNoCap(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := make([]int, 0)
		for j := 0; j < 1024; j++ {
			s = append(s, j)
		}
		sink = s
	}
}

// 预估容量：一次 make 到位，循环内只写不扩容
func BenchmarkWithCap(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := make([]int, 0, 1024)
		for j := 0; j < 1024; j++ {
			s = append(s, j)
		}
		sink = s
	}
}
```

```bash
# -run=^$ 跳过单测只跑基准，-benchmem 追加内存指标
go test -bench='NoCap|WithCap' -benchmem -run=^$ ./...
```

<OutputBox title="实测样例（Go 1.23 / darwin arm64，数值随机型波动）" output="goos: darwin  goarch: arm64\nBenchmarkNoCap-8     128374   9183 ns/op   25208 B/op   12 allocs/op\nBenchmarkWithCap-8   512906   2312 ns/op    8192 B/op    1 allocs/op" note="差距在 allocs/op：12 次对 1 次。" />

三列要会读：`ns/op` 是每次迭代耗时，`B/op` 是每次迭代分配的字节，`allocs/op` 是分配次数。`WithCap` 的 `B/op` 恰好是 1024×8=8192，说明一次到位；`NoCap` 多出来的分配次数正是 `growslice` 被调用的次数，也是 pprof 里该函数的命中次数。容量接近真实规模即可把扩容压到个位数。

<Predict no="28-4" q="把 WithCap 里的 make([]int, 0, 1024) 改成 make([]int, 0, 512)，循环仍 append 1024 次，allocs/op 最接近多少？" :options='["仍是 1 次","约 2 次","仍是 12 次","降到 0 次"]' answer="1" output="约 2 次：容量写满 512 时再扩容一次即可容纳 1024 个元素" explain="预留 512 后写满时触发一次 growslice，分配次数从 12 降到 2；容量接近真实规模即可大幅减少拷贝。" />

## 动手练一练

<Exercise title="让两个切片彻底分家" task="写一个函数，接收一个切片并返回内容与它相同、但底层数组完全独立的新切片；用取地址打印首元素地址，证明两者不共享。" :hints='["用 make 分配目标长度，再用 copy 搬数据","首元素地址可以用 &s[0] 与 %p 打印"]' solution="func detach(s []int) []int {\n    out := make([]int, len(s))\n    copy(out, s)\n    return out\n}" explain="copy 把元素写进新分配的数组，新切片 Data 指向别处；此后修改任一方都不会影响另一方。" />

## 随堂检测

<Quiz no="28-1" q="a := base[1:3] 后执行 b := append(a, 7)，若 cap(a) 足够，base 会怎样？" :options='["完全不变","base 的后续元素可能被覆盖","base 的 len 变长","一定 panic"]' answer="1" explain="cap 够用时 append 复用同一底层数组，直接写入 a 的 len 之后的位置，也就是 base 的后续槽位——这是最隐蔽的别名写。" />

<Predict no="28-2" q="s := make([]int, 2, 2); t := s; s = append(s, 3); t[0] = 99，最终 s[0] 是多少？" :options='["99","0","不确定","编译错误"]' answer="1" output="0" explain="append 时 cap 不够触发扩容，s 指向新数组；t 仍指向旧数组，改 t[0] 不影响 s。用 StepLab 一步步看这次分家。" />

<FillBlank no="28-3" tpl="full slice expression 的第三个索引把 ___ 锁住，形如 s[low:high:max]；彻底切断与底层数组的共享可以用内置函数 ___。" :answers='["cap","copy"]' hint="填一个字段名与一个内置函数" explain="第三个索引限定 cap；copy 把元素搬进新数组，不再拖住原底层数组。" />

## 一页纸速记

<KnowledgeCard :points='["切片头是 Data 指针 + Len + Cap，64 位下 24 字节，赋值只拷头","共享底层数组时改元素互相可见；append 触发扩容才「分家」","Go 1.18 把扩容阈值从 1024 改为 256，改用平滑公式，并叠加 roundupsize 对齐","full slice expression 与 copy 能锁 cap、切断共享，规避别名写与内存泄漏"]' :pitfalls='["append 的结果忘了接住，改动丢失","以为函数内 append 会改变调用方的 len（头是值拷贝）","从大数组切一小片长期持有，拖住整块内存不释放"]' summary="看切片行为，先问三件事：头指向哪、cap 够不够、有没有触发扩容。" />

## 小结与下一讲

- 切片头只有 `Data/Len/Cap` 三个字，赋值廉价但共享底层数组；`append` 是否扩容决定是否分家。
- 扩容策略有明确的版本差异：1.18 起阈值 256、平滑增长，再经 `roundupsize` 对齐，务必实测。
- `full slice expression` 锁 `cap`、`copy` 切断共享，是处理别名写与内存泄漏的两把工具。

切片是「一个数组 + 一个头」，那 `map` 呢？它为什么遍历无序、为什么并发写会直接崩、为什么删除大量键后内存不降？下一讲拆开 `hmap`。

[下一讲：map 底层：hmap、桶与渐进扩容](/core/map-internals)
