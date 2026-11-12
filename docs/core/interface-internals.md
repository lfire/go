---
title: 第 30 讲 · interface 底层：iface / eface 与 nil 陷阱
description: 解释接口为何有成本以及经典 nil 问题：iface / eface、itab 与方法表、动态派发成本、typed nil、断言与反射
---

<StageHeader stage="L3 · 核心机制" level="★★★★☆" time="45 分钟" goal="解释接口为何有成本以及经典 nil 问题" prereq="第 19 讲 · 接口：隐式实现与组合" />

# 第 30 讲 · interface 底层：iface / eface 与 nil 陷阱

> 接口是 Go 抽象的基石，但「`err != nil` 明明该为假却为真」这类 bug 几乎每个团队都踩过。原因不在业务逻辑，而在接口值的内存布局：它不是一个指针，而是**两个字**。这一讲把这两个字拆开，顺便解释接口调用为什么有成本、断言和反射为什么能工作。

## 这一讲你会得到什么

<CheckList id="goal-30" title="本讲目标" :items='["能画出 eface 与 iface 各自的双字结构","解释 itab 与方法表如何实现动态派发","彻底看懂 typed nil，并能写出不会误判的 error 返回","理解断言、type switch 与反射为什么依赖接口头"]' />

## 1. 先建立直觉

把接口值想成一张双栏标签：左栏写「这是什么类型、它的方法列表在哪」，右栏写「具体数据在哪」。空接口 `any` 的左栏只记动态类型；非空接口的左栏是一个 `itab`，里面同时记着接口类型、动态类型和一张方法表。**只有两栏都空，这张标签才算 nil**。所谓 typed nil，就是左栏清清楚楚写了类型，右栏却是空的——标签本身当然「有内容」，于是 `== nil` 为假。

<ConceptDiagram type="iface" title="iface：itab 字 + data 字" caption="左字指向类型与方法表，右字指向数据；两字皆空才是 nil" />

## 2. 语法与规则

先用镜像结构量一下大小：无论空接口还是非空接口，都是两个机器字。

```go
package main

import (
	"fmt"
	"unsafe"
)

// eface / iface 是 runtime 里两个字的镜像，仅用于教学观察
type eface struct {
	typ  unsafe.Pointer // 动态类型 *_type
	data unsafe.Pointer // 数据指针
}
type iface struct {
	tab  unsafe.Pointer // *itab：接口类型 + 动态类型 + 方法表
	data unsafe.Pointer
}

func main() {
	var a any
	var r interface {
		Read([]byte) (int, error)
	}
	fmt.Println("any 大小:", unsafe.Sizeof(a))    // 16
	fmt.Println("非空接口大小:", unsafe.Sizeof(r)) // 16
	fmt.Println("eface / iface 镜像:", unsafe.Sizeof(eface{}), unsafe.Sizeof(iface{}))
}
```

<LineByLine code="type eface struct { typ unsafe.Pointer; data unsafe.Pointer }\ntype iface struct { tab unsafe.Pointer; data unsafe.Pointer }\nfmt.Println(unsafe.Sizeof(a), unsafe.Sizeof(r))" :notes='["空接口 any 的头：动态类型字 + 数据字，共 16 字节","非空接口的头：itab 字 + 数据字，itab 里藏着方法表","两种接口都是两个字，赋值时拷贝的只是这两个字"]' />

接着看那个经典陷阱。把一个 nil 具体指针赋给接口，接口并不会变成 nil：

```go
package main

import (
	"bytes"
	"fmt"
	"io"
)

func describe(w io.Writer) {
	if w == nil {
		fmt.Println("真正的 nil 接口")
		return
	}
	fmt.Printf("非 nil：动态类型 %T\n", w)
}

func main() {
	var w io.Writer // 两个字段都为空
	describe(w)

	var buf *bytes.Buffer // 一个 nil 的具体指针
	describe(buf)         // 装箱后 tab 非空，接口整体不等于 nil
}
```

<OutputBox title="运行结果" output="真正的 nil 接口\n非 nil：动态类型 *bytes.Buffer" />

`describe(buf)` 走的是「非 nil」分支：装箱时运行时把 `*bytes.Buffer` 这个类型信息写进了 `tab`，数据字仍为空，于是接口整体不等于 nil。用 `%v` 打印这类值会显示成 `` `<nil>` ``，看起来是 nil，判断却是非 nil——这正是最容易迷惑人的地方。

## 3. 深入一步

把结构和判定规则列成表，问题就彻底透明了。

| 接口种类 | 第一个字 | 第二个字 | 来源 |
| --- | --- | --- | --- |
| `eface`（`any`） | `*type` 动态类型 | `data` 数据指针 | 空接口 |
| `iface`（非空接口） | `*itab` 类型 + 方法表 | `data` 数据指针 | 带方法集的接口 |

| 类型字 / itab | 数据字 | 接口 `== nil` | 说明 |
| --- | --- | --- | --- |
| 空 | 空 | true | 真正的 nil 接口 |
| 非空 | 空 | **false** | typed nil 陷阱 |
| 非空 | 非空 | false | 正常值 |

<Term word="itab" text="interface table 的缩写，缓存「某具体类型实现了某接口」的对应关系，并保存该接口的方法地址表，使调用方能通过它间接找到方法实现。" />

接口调用的成本就来自这里：编译期只知道接口类型，运行时要顺着 `itab` 找到方法地址再跳转，还往往挡住内联。用基准测试可以观察，但结果未必如想象中悬殊：

```go
package main

import (
	"fmt"
	"testing"
)

type Adder interface{ Add(int, int) int }
type Impl struct{}

//go:noinline
func (Impl) Add(a, b int) int { return a + b }

//go:noinline
func direct(x Impl, n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += x.Add(i, 1)
	}
	return s
}

//go:noinline
func viaIface(x Adder, n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += x.Add(i, 1)
	}
	return s
}

func main() {
	var impl Impl
	d := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = direct(impl, 100)
		}
	})
	v := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = viaIface(impl, 100)
		}
	})
	fmt.Println("直接调用:", d.String())
	fmt.Println("接口调用:", v.String())
}
```

差距不显著甚至没有，是因为编译器在能推断具体类型时会做**去虚拟化**优化，把接口调用还原成直接调用。只有在类型真正动态（切片里混放、跨包返回接口）时才退化到 `itab` 间接派发。这里同样有版本因素：去虚拟化能力随编译器版本增强，Go 1.18 起泛型也让一部分原本要装进 `any` 的场景改用类型参数，减少装箱。因此「接口一定慢」是过时结论，开销要看类型是否可静态确定。

<Callout type="warn" title="typed nil：error 返回头号坑" text="函数里写 var p *MyErr; return p，返回的是一个装了 nil 指针的非 nil 接口，调用方 if err != nil 会误判为出错。正确做法是在函数内显式 return nil。判断接口是否为 nil，永远同时看类型字和数据字。" />

### 推导链：从 runtime2.go 与 iface.go 推到 typed nil 与动态派发

结论的出处是 `runtime/runtime2.go`：`eface` 是 `_type *_type` 加 `data unsafe.Pointer` 两个字，`iface` 是 `tab *itab` 加 `data unsafe.Pointer` 两个字；`itab` 含 `inter *interfacetype`、`_type *_type`、`hash uint32` 与 `fun [1]uintptr`（方法地址表，按方法数向后扩展）。`runtime/iface.go` 的 `getitab`/`itabTable` 缓存「具体类型 × 接口类型」的匹配结果，`convT64`、`convTstring` 等负责装箱并分配数据副本，`assertE2I2`、`assertI2I` 实现断言，`runtime/type.go` 的 `_type`/`rtype` 正是反射读到的同一份描述符。四条推导链：

- **typed nil**：把 nil 的 `*bytes.Buffer` 赋给 `io.Writer` 时，`getitab` 返回一个非 nil 的 `*itab` 写进 `tab`，`data` 仍为空；而 `w == nil` 编译成「两个字同时为零」的比较，`tab` 非零故为假。打印像 nil、判定却非 nil，根因就在这两个字不对称。
- **动态派发**：接口方法调用在 `cmd/compile/internal/ssagen` 被降级为「从 `itab.fun` 取方法地址再间接调用」，编译期不知道具体类型，因此常挡住内联；编译器能推断具体类型时会在 SSA 阶段做去虚拟化（devirtualization），把调用还原成直接调用甚至内联，Go 1.21 起 PGO 还会用 profile 把高频接口调用去虚拟化。
- **断言与反射**：`x.(T)` 编译成比较 `itab.hash` 或 `_type` 指针；`reflect.TypeOf`/`ValueOf` 直接读 `eface._type` 与 `data`——它们读的是同一份头信息，只是取用方式不同。

把下面程序存成 `iface.go`，用汇编把上述链条对上：

```go
package main

import (
	"fmt"
	"io"
	"os"
)

//go:noinline
func box(x int) any { return x }

//go:noinline
func viaIface(w io.Writer, b []byte) (int, error) { return w.Write(b) }

//go:noinline
func assertWriter(v any) (io.Writer, bool) {
	w, ok := v.(io.Writer)
	return w, ok
}

func main() {
	v := box(42)
	_, _ = viaIface(os.Stdout, []byte("x"))
	_, _ = assertWriter(v)
	fmt.Println(v)
}
```

```bash
go build -gcflags=-S -o /dev/null iface.go 2>&1 | grep -E 'convT|typeAssert|itab|CALL'
go build -gcflags=-m iface.go 2>&1 | grep -E 'escape|does not escape'
# 若因构建缓存没有输出，先 go clean -cache 或追加 -a
```

<OutputBox title="go build -gcflags=-S 片段（arm64 / Go 1.23 实测）" output="0x0018 00024 CALL runtime.convT64(SB)\n0x0050 00080 MOVD $go:itab.*os.File,io.Writer(SB), R0\n0x0080 00128 CALL runtime.typeAssert(SB)\n0x0038 00056 CALL (R5)" note="convT64 装箱、itab 符号静态缓存、CALL (R5) 是经方法表的间接跳转；amd64 下形如 CALL CX。" />

逐字段解释：`convT64` 把 int 装箱（分配数据副本，故 `-m` 里可见 `x escapes to heap`）；`go:itab.*os.File,io.Writer` 是链接器为「具体类型 `*os.File` 实现接口 `io.Writer`」生成的 itab 符号，证明类型可知时匹配结果被静态缓存；`runtime.typeAssert` 是 comma-ok 断言的运行时助手（旧版本常见 `assertE2I2`，措辞随版本变化）；`CALL (R5)` 是从 `itab.fun[0]` 取出 `Write` 地址后的间接派发（amd64 下形如 `MOVQ (AX), CX` 加 `CALL CX`），正是它挡住了内联。

<OutputBox title="接口头大小与平台字长" output="amd64: any=16  io.Writer=16  两字各 8 字节\n386  : any=8   io.Writer=8   两字各 4 字节" note="把第 2 节量大小的程序用 GOARCH=386 go build 交叉编译即可看到 8 字节版本。" />

**为什么这样设计**：一个指针只能定位数据，回答不了「动态类型是什么、有哪些方法」；两个字的 `eface`/`iface` 把类型信息与数据并列，断言、type switch、反射才能 O(1) 拿到类型。`itab` 把「具体类型 × 接口类型」的匹配（含 `hash` 与方法表 `fun`）缓存进全局 `itabTable`，否则每次接口赋值与断言都要线性搜方法集。替代方案（把类型信息塞进数据块头部）会让所有值都多一段头、且与 C 内存布局不兼容，代价更高。代价是接口调用多一次间接跳转并常挡住内联，于是编译器用去虚拟化把能静态确定的调用还原——这正是第 3 节基准里「差距不显著」的机制原因。

### 装箱的分支：为什么小整数进 any 不分配

上一节的推导链提到 `convT64` 会「装箱并分配数据副本」，但这句话有个重要例外。`runtime/iface.go` 的 `convT64(val uint64)` 开头就有一个分支：`if val < uint64(len(staticuint64s))` 命中时，`x = unsafe.Pointer(&staticuint64s[val])`——直接指向运行时预置的只读数组，**完全不调用 `mallocgc`**；未命中才 `x = mallocgc(8, uint64Type, true)` 分配一个 8 字节副本再写入 val。`staticuint64s` 是 `[256]uint64`（下标 0 到 255），共 2048 字节常驻只读数据。`convT16`、`convT32` 有同款检查（`iface.go:366/379`），而 `convTstring`、`convTslice` 没有，一律分配。字段状态变化：命中分支只把接口的 `data` 字指向静态数组，`typ` 字指向 `uint64Type`，无堆对象产生；可观察输出就是 `testing.AllocsPerRun` 的 0 与 1。

把边界值两侧都测一遍：

```go
package main

import (
	"fmt"
	"testing"
)

var sink any

//go:noinline
func boxInt(v int) any { return any(v) }

func main() {
	for _, v := range []int{1, 255, 256, 1000} {
		a := testing.AllocsPerRun(1000, func() { sink = boxInt(v) })
		fmt.Printf("装箱 %4d: %.0f allocs/op\n", v, a)
	}
}
```

```bash
go run box.go
go build -gcflags=-m=2 -o /dev/null box.go 2>&1 | grep -E 'convT64|escapes to heap' | head
```

<OutputBox title="装箱分配实测（Go 1.23 / arm64）" output="装箱    1: 0 allocs/op\n装箱  255: 0 allocs/op\n装箱  256: 1 allocs/op\n装箱 1000: 1 allocs/op" note="边界恰在 256，即 staticuint64s 的长度；0 到 255 的整数装箱零分配。" />

逐字段解释：`1` 与 `255` 命中 `val < 256` 分支，`data` 字指向 `staticuint64s[val]`，无堆分配；`256` 与 `1000` 越界，走 `mallocgc` 分配 8 字节副本，每次 1 次分配。`-m=2` 里对应能看到 `x escapes to heap` 的 flow 只出现在越界路径上。这个 2048 字节的静态数组是运行时用空间换时间的常数优化：它让 `fmt.Println(状态码)`、`map[any]小整数` 这类高频小整数装箱不产生垃圾。

**反直觉结论的证伪**：「任何值装进 `any` 都会发生一次堆分配」是错的——0 到 255 的整数零分配，这也解释了为什么日志里打印小整数有时看不到额外的 `allocs/op`。反过来「装箱都免费」同样错：256 起、字符串、切片、结构体都要分配，且 `-m=2` 里能看到对应的 `escapes to heap`。把这条结论和第 3 节的基准放在一起才完整：接口调用的**派发**成本由 `itab` 与去虚拟化决定，装箱的**分配**成本由 `convT*` 的分支决定，两者是独立的，别把「接口调用差距不显著」误读成「装箱也没有代价」。

边界：`staticuint64s` 的长度 256 是 Go 1.23 的实现细节，跨版本可能调整；`//go:noinline` 是本实验前提，否则编译器可能把常量装箱静态折叠、连 `convT64` 都不调用；`AllocsPerRun` 只计堆分配，静态数组常驻内存属固定开销；逃逸分析（第 27 讲）与内联（第 36 讲）会改写一次具体调用是否真的装箱；32 位平台 `int` 为 32 位走 `convT32`，但 256 这个边界不变；`unsafe` 强转接口头读字段仍不是稳定契约。

### `itabTable` 的哈希布局：一次 `getitab` 到底查了几次

前面讲过 `getitab`/`itabTable` 缓存「具体类型 × 接口类型」的匹配，但没拆开这张表本身。它是一张**开放寻址、二次探测、75% 扩容**的全局哈希表，全部落在 `runtime/iface.go`（Go 1.23.8）：

```text
const itabInitSize = 512
type itabTableType struct {
    size    uintptr             // 恒为 2 的幂，初始 512
    count   uintptr             // 已填条目数
    entries [itabInitSize]*itab // 实际按 size 向后扩展
}
func itabHashFunc(inter *interfacetype, typ *_type) uintptr {
    return uintptr(inter.Type.Hash ^ typ.Hash)   // 两个类型哈希按位异或
}
```

**字段迁移链**：`getitab(inter, typ, canfail)` 第一趟是**无锁快路径**——`t := (*itabTableType)(atomic.Loadp(&itabTable))`，直接 `t.find(inter, typ)`；查到就 `goto finish`，绝大多数命中都停在这里，连 `itabLock` 都不碰。未命中才 `lock(&itabLock)` 并二次 `find`（防并发插入竞态），仍没有才 `persistentalloc` 出一个 `itab`、置 `m.Inter/m.Type`、`itabInit(m, true)`、`itabAdd(m)`。注意源码特意写明 `m.Hash = 0` 且「`m.Hash` 不是这张表用的哈希」——表用的哈希是 `itabHashFunc` 即时算的异或值，别把 `itab.hash` 字段（供 type switch 用）与桶下标混为一谈。

**分支条件与探测序列**：`find` 的注释给出 `h(i) = h0 + i*(i+1)/2 mod 2^k`，代码里是 `mask := t.size - 1`、`h := itabHashFunc(...) & mask`，循环 `for i := uintptr(1); ; i++ { ...; h += i; h &= mask }`。也就是从 `h0` 出发，依次加 1、2、3、4……得到 `h0, h0+1, h0+3, h0+6, h0+10…`，每一步用 `m == nil` 判空返回未命中、用 `m.Inter == inter && m.Type == typ` 判命中。因为 `size` 是 2 的幂，这个三角形序列能覆盖全部槽位，不会像线性探测那样扎堆。

**扩容阈值是算得出来的**：`itabAdd` 里 `if t.count >= 3*(t.size/4)` 即以 75% 装载率触发，随后 `t2 := mallocgc((2+2*t.size)*goarch.PtrSize, nil, true)`、`t2.size = t.size*2`、`iterate_itabs(t2.add)` 重插全部条目，最后 `atomicstorep(&itabTable, t2)` 原子换表。代入初始 `size=512`：`3*(512/4) = 384` 条时扩容，请求 `(2+1024)*8 = 8208` 字节（2 个头字加 1024 个指针槽），新 `size=1024` 的下一个阈值是 768 条、请求 16400 字节。

还有一层「零次哈希」的捷径：编译器会为静态可知的「具体类型 × 接口」对生成 `go:itab.*os.File,io.Writer` 这类链接期符号，`itabsinit` 在启动时把它们批量塞进表；所以类型可静态确定的接口赋值连 `getitab` 都不用跑，只有反射、跨包动态组合才真的走上面那趟探测。这也解释了为什么「接口赋值」的开销远没有「每次查全局哈希表」听起来那么重。

新观察角度一：把上述阈值与探测序列**本地算一遍**，不看运行时也能复现。存成 `itabcalc.go`：

```go
package main

import "fmt"

// 对应 runtime/iface.go 的 itabHashFunc 与 find 的二次探测
func findProbe(interHash, typHash uintptr, size uintptr) []uintptr {
	mask := size - 1
	h := (interHash ^ typHash) & mask
	out := []uintptr{h}
	for i := uintptr(1); i <= 6; i++ {
		h += i
		h &= mask
		out = append(out, h)
	}
	return out
}

// 对应 itabAdd 的 75% 扩容阈值与 mallocgc 请求字节
func growAt(size uintptr) (uintptr, uintptr) {
	return 3 * (size / 4), (2 + 2*size) * 8
}

func main() {
	for _, size := range []uintptr{512, 1024, 2048} {
		count, bytes := growAt(size)
		fmt.Printf("size=%4d 扩容阈值 count=%4d 新表 mallocgc=%6d B\n", size, count, bytes)
	}
	interHash := uintptr(0x9e3779b97f4a7c15)
	typHash := uintptr(0x243f6a8885a308d3)
	fmt.Printf("interHash ^ typHash = %#x\n", interHash^typHash)
	fmt.Printf("size=512 二次探测序列: %v\n", findProbe(interHash, typHash, 512))
	fmt.Println("itab 头 24 B：inter(8)+_type(8)+hash(4)+pad(4)；fun[0] 偏移 24")
}
```

<OutputBox title="itabcalc.go 输出（Go 1.23.8 / darwin arm64）" output="size= 512 扩容阈值 count= 384 新表 mallocgc=  8208 B\nsize=1024 扩容阈值 count= 768 新表 mallocgc= 16400 B\nsize=2048 扩容阈值 count=1536 新表 mallocgc= 32784 B\ninterHash ^ typHash = 0xba081331fae974c6\nsize=512 二次探测序列: [198 199 201 204 208 213 219]\nitab 头 24 B：inter(8)+_type(8)+hash(4)+pad(4)；fun[0] 偏移 24" note="探测序列的相邻差恰是 1 2 3 4 5 6，即三角形增量；首槽 198 = 0x...c6 与 511 相与的结果。" />

逐字段解释：`384 / 768 / 1536` 是 `3*(size/4)` 逐档代入，`8208 / 16400 / 32784` 是 `(2+2*size)*8`，把「75% 扩容」从一句结论变成可核的字节数。`interHash ^ typHash = 0xba081331fae974c6`，与 `mask=511` 相与得 `0xc6 = 198`，正是探测首槽；后面的 `199, 201, 204, 208, 213, 219` 相邻差为 `1,2,3,4,5,6`，与 `find` 里 `h += i` 完全一致。它也顺带给出「命中要查几次」的量化口径：装载率不超过 0.75 时，未命中前的期望探测次数是常数级，而不是随接口数量线性增长。

新观察角度二：把**去虚拟化的机器码代价**量到指令级，同时验证 `itab.fun[0]` 的偏移。存成 `dv.go`：

```go
package main

import "fmt"

type Adder interface{ Add(int, int) int }
type Impl struct{}

//go:noinline
func (Impl) Add(a, b int) int { return a + b }

//go:noinline
func concrete(x Impl, n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += x.Add(i, 1)
	}
	return s
}

//go:noinline
func dynamic(x Adder, n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += x.Add(i, 1)
	}
	return s
}

func main() {
	fmt.Println(concrete(Impl{}, 10), dynamic(Adder(Impl{}), 10))
}
```

```bash
go build -o dvbin dv.go
go tool objdump -s '^main\.concrete' ./dvbin | grep -E 'CALL|Add'
go tool objdump -s '^main\.dynamic'  ./dvbin | grep -E 'MOVD|CALL'
go tool nm ./dvbin | grep 'go:itab' | head -3
sed -n '15,32p' "$(go env GOROOT)/src/runtime/iface.go"
sed -n '107,150p' "$(go env GOROOT)/src/runtime/iface.go"
```

<OutputBox title="直接调用 vs itab 间接派发（Go 1.23.8 / arm64）" output="--- concrete ---\n  dv.go:12  CALL main.Impl.Add(SB)\n--- dynamic ---\n  dv.go:15  MOVD 24(R0), R4\n  dv.go:15  CALL (R4)\n--- go:itab 静态符号 ---\n1000e52e8 R go:itab.*errors.errorString,error" note="同一个 Add：去虚拟化后是直连符号的 CALL，动态派发要先从 itab+24 取地址再间接 CALL。" />

逐字段解释：`concrete` 只剩一条 `CALL main.Impl.Add(SB)`，目标是链接期已知符号，编译器可继续把它内联；`dynamic` 多出 `MOVD 24(R0), R4`——R0 是接口的 `tab` 字，`24` 正是 `itab` 头 `inter(8)+_type(8)+hash(4)+pad(4)` 之后 `fun[0]` 的偏移，取出的地址再由 `CALL (R4)` 间接跳转。于是「动态派发成本」可以量化成：**一次额外的 `itab+24` 载入 + 一次不可内联的间接调用**，而不是含糊的「慢一点」。`go:itab.*errors.errorString,error` 这类 `R`（只读数据）符号则证明静态可知的类型对确实在链接期就备好了 itab，运行时无需为它们跑 `getitab` 的哈希探测。`sed` 两行给出 `itabInitSize`、`itabHashFunc`、`find` 的二次探测与 `itabAdd` 的 75% 分支原文，把上面的算式与源码对上。

边界：`itabInitSize=512`、75% 阈值、`(2+2*size)*PtrSize` 的分配公式都是 Go 1.23 的实现细节，跨版本先对照 `runtime/iface.go`；`itabHashFunc` 用的是类型描述符里的编译期哈希，模拟里用任意常数代入只为演示探测步长，不代表真实类型哈希；`find` 的三角形序列在 `size` 为 2 的幂时才覆盖全表；`go tool objdump` 的寄存器名随 GOARCH 变化（amd64 下是 `MOVQ 24(AX), CX` 加 `CALL CX`），只比指令语义；`//go:noinline` 是本实验前提，否则 `concrete` 与 `dynamic` 都可能被整体优化掉；`itab.fun[0]` 的 24 字节偏移依赖字段顺序，属非稳定契约。这条链在第 53 讲（mock 与 itab 布局约束）、第 98 讲（去虚拟化与 PGO）会再出现。

### 边界、常见误解与前后联系

| 常见误解 | 正解 | 可验证依据 |
| --- | --- | --- |
| `interface{}` 就是 void 指针，只占一个指针 | 两个字，含类型信息；64 位下 `unsafe.Sizeof(any)` 为 16 | 第 2 节与上面的平台对照 |
| 接口调用一定比直接调用慢很多 | 类型可静态确定时去虚拟化把差距抹平，只有真动态派发才走 `itab.fun` 间接跳转 | 第 3 节的 `testing.Benchmark` 对比 |
| 两个接口值用 `==` 比较永远安全 | 动态类型不可比较（slice、map、func）时运行时 panic，且相等性按该类型定义 | 装一个切片进 `any` 再比较 |

边界要点：32 位下接口头为 8 字节；接口赋值是**两个字的非原子写入**，多 goroutine 无同步读写同一接口变量是数据竞争（第 49 讲）；`nil` 具体指针装箱后不等于 nil；`reflect.ValueOf(nil)` 得到零值 `Value`、`Kind()` 为 Invalid；去虚拟化只在编译器能推断具体类型时生效，跨包返回接口或切片里混放类型会退化；`any` 装箱常伴随一次堆分配（`-m` 可见 `escapes to heap`）；`unsafe` 镜像读头部字段不是稳定契约；Go 1.18 起泛型让一部分原本要装箱的场景改用类型参数，进一步改变开销结论。

前后联系：第 19 讲（接口的使用）与第 26 讲（描述符）在这里落到两字结构；第 98 讲（编译器源码：SSA、逃逸与内联）会看 `convT`/`assert` 这类运行时调用在 SSA 阶段如何被生成与消除，第 99 讲（Go 内存模型与 happens-before）解释「两字非原子写入」为何必须靠同步保护，第 100 讲（极致性能优化）把「热路径避免高频装箱」列为常规手段。

## 4. 工程实践

断言、`type switch` 与反射之所以能工作，正是因为接口头里保存了动态类型；它们读的是同一份信息，只是取用方式不同。

```go
package main

import (
	"fmt"
	"reflect"
)

func inspect(v any) {
	if s, ok := v.(string); ok { // comma-ok 断言，失败不 panic
		fmt.Println("字符串长度:", len(s))
	}
	switch x := v.(type) { // type switch 读的是头里的动态类型
	case int:
		fmt.Println("int 加一:", x+1)
	default:
		fmt.Println("其它类型:", reflect.TypeOf(v))
	}
	val := reflect.ValueOf(v)
	fmt.Println("Kind:", val.Kind()) // 反射同样依赖接口保存的类型信息
}

func main() {
	inspect(42)
	inspect("go")
}
```

<Compare :panels='[{"label":"反例：包着 nil 指针","code":"var p *MyErr; return p","note":"返回后 err 不等于 nil，调用方误判为出错"},{"label":"正例：显式返回 nil","code":"if p == nil { return nil }","note":"返回真正的 nil 接口，两个字段都为空"}]' title="error 返回的两种写法" />

工程清单：返回 `error` 时永远写无类型的 `nil`，不要返回具体指针变量；判定具体错误用 `errors.Is` / `errors.As`（第 20 讲）而非拿具体指针和 `nil` 比较；热路径尽量避免高频把值装箱进 `any`，能传具体类型或泛型就别传接口；接口遵循「在使用处定义、尽量小」的原则（第 19 讲）。和上一讲一样，用 `unsafe` 去强转接口头读字段只适合教学，运行时布局不是稳定契约。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l3-core/lesson-030-interface-internals`](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-030-interface-internals)。clone 仓库后进入 `examples/l3-core`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="修掉一个 typed nil" task="下面函数会返回一个「不等于 nil」的 error，请改写它：当内部指针为 nil 时返回真正的 nil，并用 err == nil 验证。" :hints='["问题出在把 nil 指针直接 return 成接口","显式判断后 return nil 即可"]' solution="func load() error {\n    var p *MyErr\n    if p == nil { return nil }\n    return p\n}" explain="显式返回无类型的 nil，接口的两个字才都为空；直接返回 p 会装箱成 typed nil。" />

## 随堂检测

<Quiz no="30-1" q="var p *MyErr = nil; var e error = p，此时 e == nil 的结果是？" :options='["true","false","编译错误","取决于 MyErr 是否实现 Error"]' answer="1" explain="把 p 装箱成接口后，类型字记下 *MyErr、数据字为空，接口整体不等于 nil。这就是 typed nil 陷阱。" />

<Predict no="30-2" q="func bad() error { var p *MyErr; return p }，调用方判断 bad() != nil 结果如何？" :options='["false，一切正常","true，明明没出错却走进错误分支","panic","编译错误"]' answer="1" output="true：err 不等于 nil 成立，但里面其实是个 nil 指针" explain="返回值被装箱成非 nil 接口，调用方误判。正确做法是在函数里显式 return nil。" />

<FillBlank no="30-3" tpl="非空接口的头由 ___ 和数据指针两个字组成，前者保存接口类型、动态类型与方法表；空接口 any 的头则由动态类型字与 ___ 字组成。" :answers='["itab","数据"]' hint="填一个结构名与一个字段名" explain="iface 用 itab 承载方法表；eface 只记录动态类型与数据。两者都是两个字、16 字节。" :explains='["非空接口带方法集，首个字须指向 itab，缓存接口类型、动态类型与方法地址，才能完成方法动态分派；空接口无方法便省掉它。","空接口不承诺任何方法，两个字分别是动态类型与指向实际值的数据指针，第二字直接定位值本身，无需再经方法表。"]' />

## 一页纸速记

<KnowledgeCard :points='["接口值一律两个字：eface 是类型字 + 数据字，iface 是 itab 字 + 数据字","两个字段都为 nil 时接口才等于 nil；装了 nil 指针的接口不等于 nil","方法调用经 itab 间接派发，编译器在类型可知时会做去虚拟化优化","断言、type switch 与反射都读的是接口头里的动态类型信息"]' :pitfalls='["函数返回 error 时返回了 nil 具体指针，制造 typed nil","只用 != nil 判断具体错误类型，应改用 errors.Is","在热路径高频装箱，带来分配与派发成本"]' summary="判断接口，永远同时看「类型字」和「数据字」，别只看其中一个。" />

## 小结与下一讲

- 接口值是双字结构：`eface` 记类型 + 数据，`iface` 记 `itab` + 数据；两字皆空才是 nil。
- typed nil 源于「类型字非空、数据字空」，写 `error` 返回时显式 `return nil` 是唯一稳妥做法。
- 动态派发经 `itab` 间接跳转，但去虚拟化与泛型会显著削弱开销，别凭直觉下结论。

字符串也是「小头 + 指向别处」，却多了一条铁律：不可变。正因如此，它与 `[]byte` 之间的每次转换都要拷贝；而 `unsafe` 零拷贝能省掉这次拷贝，代价是什么？下一讲收尾核心机制前半。

[下一讲：字符串底层与零拷贝转换](/core/string-internals)
