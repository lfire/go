---
title: 第 028 讲 · 切片底层：三元组、扩容与共享陷阱
description: 彻底掌握 slice 的内存行为：SliceHeader、共享底层数组、扩容策略版本差异、append 别名陷阱与 full slice expression
---

<StageHeader stage="L3 · 核心机制" level="★★★★☆" time="45 分钟" goal="彻底掌握 slice 的内存行为" prereq="第 027 讲 · 逃逸分析与编译器决策" />

# 第 028 讲 · 切片底层：三元组、扩容与共享陷阱

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

<Callout type="warn" title="append 的三条铁律" text="一，结果一定要接住并传回给所有持有方；二，把切片传进函数后 append，不会改变调用方的 len——头是值拷贝（回看第 026 讲）；三，从大数组切一小片长期持有，会拖住整块底层数组无法回收，是常见的内存泄漏。" />

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

### 设计动机与替代方案的代价

为什么是「描述符加共享底层数组」，而不是让切片值直接装下整个数组？若值自带数组，`s2 := s1` 与函数传参都要 O(n) 拷贝，`append` 也无法在不通知所有持有方的前提下换一块更大的内存；共享把传参压到常数级的三个字，代价就是别名写，于是 `cap` 与 `full slice expression` 被设计成「是否共享」的显式开关。为什么不做写时复制（COW）或自动缩容？那需要每次写都检查引用计数或加锁，恰好破坏 Go 最看重的无锁热路径；Go 选择把成本交给程序员用 `copy`、三索引表达式显式承担，编译器只保证 `growslice` 一次搬到位。为什么不省掉 `Cap`？没有它就无从判断 `append` 能否原地写入，也无法用第三个索引把容量锁死。这三种取舍合起来，才推出「头只有三个字、共享恒定存在、扩容才分家」的全部行为。

<OutputBox title="头部字段与平台字长（hdr.go 输出）" output="GOARCH=amd64  IntSize=64\n头大小=24  Data/Len/Cap 偏移=0 8 16\nnil:   Data=nil        len=0 cap=0\nempty: Data=0xc0000120a0 len=0 cap=0\nGOARCH=386 : 头大小=12  偏移=0 4 8" note="三个字各占一个指针宽度；nil 与空切片的 Data 不同但 len、cap 都是 0。" />

```go
package main

import (
	"fmt"
	"runtime"
	"strconv"
	"unsafe"
)

type hdr struct {
	Data unsafe.Pointer
	Len  int
	Cap  int
}

func main() {
	var h hdr
	fmt.Println("GOARCH:", runtime.GOARCH, " IntSize:", strconv.IntSize)
	fmt.Println("头大小:", unsafe.Sizeof(h))
	fmt.Println("Data/Len/Cap 偏移:",
		unsafe.Offsetof(h.Data), unsafe.Offsetof(h.Len), unsafe.Offsetof(h.Cap))
	var nilS []int
	emptyS := make([]int, 0)
	fmt.Printf("nil Data=%v len=%d cap=%d\n",
		(*hdr)(unsafe.Pointer(&nilS)).Data, len(nilS), cap(nilS))
	fmt.Printf("empty Data=%v len=%d cap=%d\n",
		(*hdr)(unsafe.Pointer(&emptyS)).Data, len(emptyS), cap(emptyS))
}
```

命令：`go run hdr.go` 看本机结果，`GOARCH=386 go build hdr.go` 交叉编译确认 32 位布局；`go build -gcflags=-S -o /dev/null slice.go 2>&1 | grep growslice` 再把 `append` 与 `runtime.growslice` 的调用对上（若因构建缓存无输出，加 `-a` 强制重编）。逐字段解释：`IntSize` 决定三个字段各占 4 还是 8 字节，故 amd64 是 24、386 是 12；`unsafe.Offsetof` 给出 0、8、16（386 下 0、4、8），证明头是紧凑排列的三个字；`nil` 切片的 `Data` 为 nil，`make([]int, 0)` 的空切片 `Data` 指向运行时的 `zerobase` 而非 nil，但两者 `len`、`cap` 都是 0，也都能直接 `append`。`unsafe.Sizeof` 与手写镜像都**只测头部、不测底层数组**，这是它们作为观测手段的硬边界；`reflect.SliceHeader.Data` 是 `uintptr`、不参与 GC 存活判定（Go 1.21 起已标记弃用），别拿它长期保存地址。

### growslice 的 required 分支与容量的二次取整

上一节把 512 之后跳到 848 归因于「平滑公式加 roundupsize」，但那条推导只覆盖**一次追加一个元素**。一旦 `append` 一次塞进很多元素，`runtime/slice.go` 的 `growslice` 会走另一条分支：`nextslicecap(oldcap, required)` 先以 `oldcap*2` 起步，Go 1.18 起当 `oldcap >= 256` 时改成 `newcap += (newcap + 3*256)/4` 平滑迭代；关键的一步是 **`newcap` 若仍小于本次所需容量 `required`，就直接取 `required`**，而不是继续按倍数涨。随后 `roundupsize(uintptr(newcap) * et.Size)` 把字节数对齐到 size class，再除以 `et.Size` 得到最终 `cap`，接着 `mallocgc` 分配、`memmove` 搬旧元素。可观察输出就是一次 `append(s, extra...)` 之后的 `cap`。

把不同初始容量、一次追加 1000 个 `int` 的结果摆在一起，分支条件一目了然：

```go
package main

import "fmt"

func main() {
	for _, base := range []int{0, 4, 8, 100, 600} {
		s := make([]int, base, base)
		extra := make([]int, 1000)
		r := append(s, extra...)
		fmt.Printf("oldcap=%4d 一次 append 1000 个 → newcap=%4d\n", base, cap(r))
	}
}
```

```bash
go version
go run grow.go
# 换成 []int8 或 32 位架构，roundupsize 的档位不同，最终 cap 会变
GOARCH=386 go run grow.go
```

<OutputBox title="一次 append 1000 个 int 的实测（Go 1.23 / arm64）" output="oldcap=   0 一次 append 1000 个 → newcap=1024\noldcap=   4 一次 append 1000 个 → newcap=1024\noldcap=   8 一次 append 1000 个 → newcap=1024\noldcap= 100 一次 append 1000 个 → newcap=1184\noldcap= 600 一次 append 1000 个 → newcap=1696" note="前三行 required 超过两倍旧容量，公式分支被短路，容量直接跳到所需值再取整。" />

逐字段解释：算式是 `cap_final = roundupsize(max(newcap, required) × et.Size) / et.Size`。前三行 `oldcap` 为 0/4/8 时 `required` 是 1000/1004/1008，都远大于 `2×oldcap`，于是 `newcap = required`，换算成 8000/8032/8064 字节后统一 `roundupsize` 到 8192 字节档，除回 8 得 1024。`oldcap=100` 时 `required=1100 > 200`，同样短路到 1100 个元素、8800 字节，对齐到 9472 字节档得 1184；`oldcap=600` 时 `required=1600 > 1200`，12800 字节对齐到 13568 字节档得 1696。可见「翻倍 / 平滑公式」只在所需容量不超过两倍时起作用，超过就由 `required` 接管，最后再叠一次 size class 取整——这是「打印出的 cap 总对不上公式」的第二个原因，第一个是上一节讲的 256 阈值与平滑增长。

**反直觉结论的证伪**：「append 的容量只由扩容策略决定，与一次追加多少无关」是错的。对照 `oldcap=8` 一次追加 1 个元素会得到 16（翻倍），一次追加 1000 个却直接得到 1024；同一起点、不同追加量，容量差了 64 倍。另一个易错点是「公式算出的 cap 就是最终 cap」——`roundupsize` 的二次取整在三行里分别把 1000/1004/1008 压成同一个 1024、把 1100 抬到 1184，忽略它就会误判预分配该给多少容量。工程上因此 `make([]T, 0, n)` 的 `n` 应尽量贴合真实规模：给小了触发 `growslice`，给大了白占一个更大的档位。

边界：`nextslicecap` 是 Go 1.21 起抽出的函数，Go 1.18 前后阈值从 1024 改为 256，跨版本先 `go version`；`et.Size` 随元素类型变化，`[]int8`、`[]string` 的字节数与档位都不同；32 位平台 `int` 为 4 字节、`roundupsize` 档位表不同，最终 cap 会变；`append` 到三索引切片或已满 cap 的子切片行为不同；这些是运行时实现细节，结论以本机实测为准，不能写进业务逻辑。

### 含指针元素扩容：写屏障辅助搬移到底走哪条分支

第 3 节提过「元素含指针时还要走写屏障辅助路径」，但只给了结论，没展开 `growslice` 里那个分支改了哪些字段、为什么叫「辅助」。先把一个版本事实说清：候选里常写的「`typedmemmove` / `memmove` 分流」是更早资料里的笼统说法，**本机 Go 1.23.8 的 `runtime/slice.go` 里 `growslice` 并没有调用 `typedmemmove`**，两条分支最后都调同一个 `memmove`，差别在搬移**之前**多做的那一步。`typedmemmove` 在 1.23 里服务于「单个带类型值的拷贝」——`runtime/iface.go` 的 `convT`、`runtime/map.go` 的 `mapassign`、`runtime/chan.go` 的收发各有一处；整个数组块的批量搬移则由 `bulkBarrierPreWriteSrcOnly` 承担。别把这两个符号混为一谈。

**状态迁移链**（以 Go 1.23.8 `runtime/slice.go` 的 `growslice` 为准）：输入是旧数组指针 `oldPtr`、旧字节数 `lenmem`、元素类型 `et` 与轮过 `roundupsize` 的 `capmem`。

```text
步骤 1  capmem = roundupsize(capmem, noscan)；newcap = capmem / et.Size_
步骤 2  分支 A：et.Pointers() 为假
          p = mallocgc(capmem, nil, false)          // noscan，不清零整块
          memclrNoHeapPointers(p+newlenmem, capmem-newlenmem)
        分支 B：et.Pointers() 为真
          p = mallocgc(capmem, et, true)            // scan，且必须清零
          若 lenmem > 0 且 writeBarrier.enabled：
              bulkBarrierPreWriteSrcOnly(p, oldPtr, lenmem-et.Size_+et.PtrBytes, et)
步骤 3  memmove(p, oldPtr, lenmem)                  // 两分支共用，按字节整块搬
步骤 4  返回 slice{p, newLen, newcap}
```

两个分支条件都值得单独推。其一，`et.Pointers()` 由元素类型的指针位图决定，直接决定 `mallocgc` 的 `typ` 参数与 span 是否 `noscan`；含指针时第二参数传 `et`、第三参数 `needzero` 传 `true`，源码注释写明「不能用避免清零的 rawmem，否则 GC 会扫到未初始化内存」——新数组先整体清零，才允许后面被扫描。其二，`writeBarrier.enabled` 只在并发标记期为真，所以「含指针」并不等于「每次扩容都跑屏障」，还叠一个 GC 阶段条件。

**为什么是 `SrcOnly`**：混合写屏障要求「被覆盖的旧值」被染灰。这里目标数组 `p` 刚由 `mallocgc` 清零、里面全是 nil 指针，没有旧值需要保护；真正被「覆盖/丢弃」的是**旧数组 `oldPtr` 里的指针**。`runtime/mbitmap.go` 的 `bulkBarrierPreWriteSrcOnly` 因此只遍历源区间：`s.typePointersOfType(typ, dst)` 按 `et` 的指针位图依次给出每个指针槽地址，对每个 `addr` 取 `srcx = addr - dst + src`，`p := buf.get1(); p[0] = *srcx` 把源指针推进当前 P 的 `wbBuf`。工作量正比于**被搬移区间里的指针字数**，而不是元素总数——第三个参数 `lenmem - et.Size_ + et.PtrBytes` 把扫描范围截到「最后一个指针字」为止：`[]*int` 的 `et.Size_ = et.PtrBytes = 8`，代入后就是 `lenmem`；而 `{int64; *int; int64}` 这类元素，`PtrBytes` 只覆盖到中间那个指针，尾部标量不计入屏障遍历。

新观察角度：用编译器的 `-d=wb=1` 直接打印**哪些源码行被插入了写屏障**，再反汇编链接后的 `runtime.growslice` 看分支落点。存成 `wb.go`（两个 `make` 都只留在局部，避免把切片写进全局变量引入额外屏障，干扰对照）：

```go
package main

//go:noinline
func makePtr(n int) []*int {
	s := make([]*int, 0, n)
	for i := 0; i < n; i++ {
		s = append(s, new(int))
	}
	return s
}

//go:noinline
func makeVal(n int) []int {
	s := make([]int, 0, n)
	for i := 0; i < n; i++ {
		s = append(s, i)
	}
	return s
}

func main() {
	a, b := makePtr(64), makeVal(64)
	println(len(a), len(b))
}
```

```bash
go build -gcflags='-d=wb=1' -o /dev/null wb.go
go build -gcflags='-d=append=1' -o /dev/null wb.go
go build -o wbbin wb.go
go tool objdump -s '^runtime\.growslice' ./wbbin | grep -E 'bulkBarrier|memclrNoHeapPointers|memmove|mallocgc'
# 对照源码行号
sed -n '262,284p' "$(go env GOROOT)/src/runtime/slice.go"
grep -n 'func bulkBarrierPreWriteSrcOnly' "$(go env GOROOT)/src/runtime/mbitmap.go"
```

<OutputBox title="-d=wb 与 growslice 反汇编（Go 1.23.8 / darwin arm64）" output="# command-line-arguments\n./wb.go:9:38: write barrier\n--- -d=append=1 ---\n./wb.go:9:5: append: len-only update (in local slice)\n--- go tool objdump -s '^runtime.growslice' ---\n  slice.go:272  CALL runtime.mallocgc(SB)\n  slice.go:280  CALL runtime.bulkBarrierPreWriteSrcOnly(SB)\n  slice.go:264  CALL runtime.mallocgc(SB)\n  slice.go:269  CALL runtime.memclrNoHeapPointers(SB)\n  slice.go:283  CALL runtime.memmove(SB)" note="只有含指针的 append 行被插入屏障；运行时里 scan 分支多一次 bulkBarrier 调用，两分支共用 memmove。" />

逐字段解释：`-d=wb=1` 只有一行 `./wb.go:9:38: write barrier`，定位到 `makePtr` 里 `append(s, new(int))` 的指针写；`makeVal` 所在行没有输出，证明「是否插屏障」由元素类型而非 `append` 这个动作决定。`-d=append=1` 的 `append: len-only update` 说明容量足够时只更新头的 `Len`，与第 2 节「不扩容就原地写」对上。反汇编里 `slice.go:280` 的 `CALL runtime.bulkBarrierPreWriteSrcOnly` 是分支 B 独有的辅助步骤，`slice.go:269` 的 `memclrNoHeapPointers` 属于分支 A，`slice.go:283` 的 `memmove` 两条分支共用——把「源码分支 → 运行时符号 → 机器码」三段接成一条链。`sed -n '262,284p'` 给出的正是上面状态迁移链里 `et.Pointers()` 判断与 `lenmem > 0 && writeBarrier.enabled` 复合条件的原文。

**反直觉的一点**：「含指针的切片扩容一定更慢、因为每个元素都要过屏障」不准确。额外成本是一次按指针位图遍历的 `bulkBarrierPreWriteSrcOnly`，只在 `writeBarrier.enabled` 为真时发生；`memmove` 的字节拷贝量与 `[]int` 完全相同。真正拉开差距的是 `mallocgc(capmem, et, true)` 的清零与后续 GC 对该 span 的扫描（回看第 032 讲 `noscan` 位），而不是「每个指针单独一次屏障调用」——`wbBuf.get1()` 是批量缓冲写入。这也解释了为什么第 033 讲强调「降指针密度」：`[]*T` 扩容时同时付出 scan span、清零与屏障遍历三重代价。

边界：符号名与分支实现以 Go 1.23.8 为准，`typedmemmove` 在本机属于单个值拷贝路径、不是 growslice 的分流；`bulkBarrierPreWriteSrcOnly` 的 `size` 算式依赖 `et.PtrBytes`，复合结构体与 `[]*int` 的遍历长度不同；`writeBarrier.enabled` 只在并发标记期为真，非 GC 期含指针扩容也不跑这一步；`go tool objdump -s` 的地址随构建变化、只比符号与源码行号；`//go:noinline` 是本实验对照成立的前提。这条链在第 032 讲（noscan span）、第 033 讲（混合写屏障与降指针密度）会再次合流。

### 边界、常见误解与前后联系

| 常见误解 | 正解 | 可验证依据 |
| --- | --- | --- |
| `len(s)` 等于 `cap(s)` 时 `append` 一定让所有别名立刻分家 | 只有持有新头的变量指向新数组，其它别名仍指向旧数组且旧数据不变；旧数组要等引用归零才被 GC 回收 | 第 2 节 `base` 与 `c` 的地址对比 |
| `full slice expression` 的第三个索引可以任意大 | `max` 必须落在 `high` 到 `cap` 之间，越界 panic；它只能缩小 `cap`，不能放大 | 把 `max` 写成大于 `cap(s)` 的值会触发运行时 panic |
| `nil` 切片不能 `append`，必须先 `make` | `append(nil, x)` 合法，会分配底层数组；`nil` 与空切片在读写上几乎等价 | 上面的 `hdr.go` 输出与 `append(nil, 1)` |

边界要点：32 位平台下头只有 12 字节，所有偏移、`roundupsize` 档位与容量公式都要重算；并发对同一底层数组 `append` 是数据竞争（`-race` 能抓，第 049 讲）；`cap` 经 `runtime/msize.go` 的 `roundupsize` 对齐，随元素类型、对齐要求与 GOARCH 变化，**不能当公式值**；`full slice expression` 的 `max` 受 `cap` 约束；`unsafe.Sizeof`、手写镜像与 `-m` 都是静态观察，看不到运行时扩容次数，必须叠加 `go test -benchmem` 与 `GODEBUG=gctrace=1`。还要注意子切片长期持有会拖住整块大数组不释放，这是共享的代价而非 bug。

前后联系：第 026 讲「描述符」在这里落到具体三个字，第 027 讲解释了 `make` 为何逃逸到堆，第 029 讲的 map 是另一种「小头指向别处」；第 098 讲（编译器源码：SSA、逃逸与内联）会看 `append` 在 `cmd/compile/internal/walk` 阶段如何被降级成 `runtime.growslice` 调用、以及内联与边界检查消除怎样影响扩容判断，第 100 讲（极致性能优化）把 `make([]T, 0, n)` 与 Go 1.21 的 `slices.Grow` 列为标准优化项，第 097 讲（channel/mutex 源码与锁竞争）解释并发切片为何必须靠外部同步。

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

落地建议：构造切片时尽量 `make([]T, 0, n)` 预估容量，既省扩容又少一次堆分配（第 027 讲）；函数若可能扩容，就把新切片作为返回值传回，别指望调用方「自动看见」；需要把子切片交给缓存、队列或 goroutine 长期持有时，先 `copy` 或复用三索引表达式，避免把整个大数组钉在内存里。排查线上「数据被莫名改写」时，先画一遍 `Data` 指向与 `cap`，八成是别名写在作祟。

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

<Predict q="把 WithCap 里的 make([]int, 0, 1024) 改成 make([]int, 0, 512)，循环仍 append 1024 次，allocs/op 最接近多少？" :options='["仍是 1 次","约 3 次","仍是 12 次","降到 0 次"]' answer="1" output="约 3 次：512→848→1280 触发两次 growslice，加上初始 make 共 3 次分配" explain="预留 512 后写满时先扩到 848，仍装不下 1024，需再扩到 1280，共两次 growslice，分配次数从 12 降到 3；容量接近真实规模即可大幅减少拷贝。" />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l3-core/lesson-028-slice-internals`](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-028-slice-internals)。clone 仓库后进入 `examples/l3-core`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="让两个切片彻底分家" task="写一个函数，接收一个切片并返回内容与它相同、但底层数组完全独立的新切片；用取地址打印首元素地址，证明两者不共享。" :hints='["用 make 分配目标长度，再用 copy 搬数据","首元素地址可以用 &s[0] 与 %p 打印"]' solution="func detach(s []int) []int {\n    out := make([]int, len(s))\n    copy(out, s)\n    return out\n}" explain="copy 把元素写进新分配的数组，新切片 Data 指向别处；此后修改任一方都不会影响另一方。" />

## 随堂检测

<Quiz no="028-1" q="a := base[1:3] 后执行 b := append(a, 7)，若 cap(a) 足够，base 会怎样？" :options='["完全不变","base 的后续元素可能被覆盖","base 的 len 变长","一定 panic"]' answer="1" explain="cap 够用时 append 复用同一底层数组，直接写入 a 的 len 之后的位置，也就是 base 的后续槽位——这是最隐蔽的别名写。" />

<Predict no="028-2" q="s := make([]int, 2, 2); t := s; s = append(s, 3); t[0] = 99，最终 s[0] 是多少？" :options='["99","0","不确定","编译错误"]' answer="1" explain="append 时 cap 不够触发扩容，s 指向新数组；t 仍指向旧数组，改 t[0] 不影响 s。用 StepLab 一步步看这次分家。" />

<FillBlank no="028-3" tpl="full slice expression 的第三个索引把 ___ 锁住，形如 s[low:high:max]；彻底切断与底层数组的共享可以用内置函数 ___。" :answers='["cap","copy"]' hint="填一个字段名与一个内置函数" explain="第三个索引限定 cap；copy 把元素搬进新数组，不再拖住原底层数组。" :explains='["三索引切片的第三个数限定新切片的容量上界，使其无法再 append 到覆盖 max 之后的底层元素，从而把可见范围锁住。","这个内置函数把元素逐个搬进另一个底层数组，源与目标此后互不影响；由于不再引用原数组，旧的大数组也能被 GC 回收。"]' />

## 一页纸速记

<KnowledgeCard :points='["切片头是 Data 指针 + Len + Cap，64 位下 24 字节，赋值只拷头","共享底层数组时改元素互相可见；append 触发扩容才「分家」","Go 1.18 把扩容阈值从 1024 改为 256，改用平滑公式，并叠加 roundupsize 对齐","full slice expression 与 copy 能锁 cap、切断共享，规避别名写与内存泄漏"]' :pitfalls='["append 的结果忘了接住，改动丢失","以为函数内 append 会改变调用方的 len（头是值拷贝）","从大数组切一小片长期持有，拖住整块内存不释放"]' summary="看切片行为，先问三件事：头指向哪、cap 够不够、有没有触发扩容。" />

## 小结与下一讲

- 切片头只有 `Data/Len/Cap` 三个字，赋值廉价但共享底层数组；`append` 是否扩容决定是否分家。
- 扩容策略有明确的版本差异：1.18 起阈值 256、平滑增长，再经 `roundupsize` 对齐，务必实测。
- `full slice expression` 锁 `cap`、`copy` 切断共享，是处理别名写与内存泄漏的两把工具。

切片是「一个数组 + 一个头」，那 `map` 呢？它为什么遍历无序、为什么并发写会直接崩、为什么删除大量键后内存不降？下一讲拆开 `hmap`。

[下一讲：map 底层：hmap、桶与渐进扩容](/core/map-internals)
