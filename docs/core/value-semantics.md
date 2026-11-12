---
title: 第 26 讲 · 值语义深潜：赋值、传参、拷贝成本
description: 从机器视角看懂 Go 的默认行为：一切皆值、描述符拷贝、大对象成本与编译器优化
---

<StageHeader stage="L3 · 核心机制" level="★★★☆☆" time="35 分钟" goal="从机器视角理解 Go 的默认行为" prereq="第 09 讲 · 栈、堆、值语义与指针" />

# 第 26 讲 · 值语义深潜：赋值、传参、拷贝成本

> 写了几十讲「怎么用」，从这一讲起要回答「为什么会这样」。同一个 `=` 号，作用在结构体和切片上结果完全不同；同一个函数，传值和传指针的性能也未必如直觉。把这些差异归到一条原理上——Go 里一切赋值与传参默认都是**值拷贝**——后面切片、map、interface、字符串的底层行为就都能推出来，而不用死记。

## 这一讲你会得到什么

<CheckList id="goal-26" title="本讲目标" :items='["能一句说清 Go 为什么没有「引用传递」","能区分「值里装数据」和「值里装描述符」两类类型","会用 unsafe.Sizeof 量化一次拷贝的成本","知道编译器优化与 Go 1.17 寄存器 ABI 怎样影响结论"]' />

## 1. 先建立直觉

把变量想成一张卡片。普通结构体、数组这类类型，卡片上直接写满了全部数据，复印一张就是一份完整副本，改副本和原件无关。而切片、字符串、map、接口这类类型，卡片上只写着「数据在哪、有多长」——一个**描述符**（header），真正的仓库在别处。复印这张卡片只花几字节，但两张卡片指向同一个仓库，谁改了仓库里的货，另一张卡片也看得见。

<ConceptDiagram type="value-pointer" title="值拷贝 vs 描述符拷贝" caption="左边是整份数据被复制，右边是只复制指向数据的头" />

这条分界线，是理解本阶段所有底层结构的钥匙。

## 2. 语法与规则

先看一个把三种情况放在一起的程序。请留意：同样写 `b := a`，结构体和数组互不影响，切片却「联动」了。

```go
package main

import "fmt"

// Point 的字段全是值，赋值时会整份复制
type Point struct {
	X, Y int
	Tag  string
}

func main() {
	a := Point{X: 1, Y: 2, Tag: "origin"}
	b := a // 结构体赋值：整份拷贝
	b.X = 100
	fmt.Println("a =", a, " b =", b) // a 仍是原值，b 是独立副本

	arr1 := [3]int{1, 2, 3}
	arr2 := arr1 // 数组是值：3 个 int 全部拷贝
	arr2[0] = 99
	fmt.Println("arr1 =", arr1, " arr2 =", arr2)

	s1 := []int{1, 2, 3}
	s2 := s1 // 切片是描述符：只拷贝头，底层数据共享
	s2[0] = 99
	fmt.Println("s1 =", s1, " s2 =", s2) // s1[0] 也变成 99
}
```

<OutputBox title="运行结果" output="a = {1 2 origin}  b = {100 2 origin}\narr1 = [1 2 3]  arr2 = [99 2 3]\ns1 = [99 2 3]  s2 = [99 2 3]" />

传参是同一条规则的延伸：函数拿到的一定是实参的一份拷贝，区别在于拷贝的是「整份数据」还是「描述符」。

```go
package main

import "fmt"

type Big struct{ data [4]int }

func byValue(b Big)    { b.data[0] = 9 } // 改的是副本
func byPtr(b *Big)     { b.data[0] = 9 } // 改的是原对象
func byArray(a [4]int) { a[0] = 9 }      // 改的是数组副本
func bySlice(s []int)  { s[0] = 9 }      // 改的是共享底层数组

func main() {
	b, a := Big{}, [4]int{}
	s := []int{0, 0, 0, 0}
	byValue(b)
	fmt.Println("byValue 后:", b.data[0]) // 0
	byArray(a)
	fmt.Println("byArray 后:", a[0]) // 0
	bySlice(s)
	fmt.Println("bySlice 后:", s[0]) // 9
	byPtr(&b)
	fmt.Println("byPtr 后:", b.data[0]) // 9
}
```

<LineByLine code="func byValue(b Big) { b.data[0] = 9 }\nfunc bySlice(s []int) { s[0] = 9 }\nbyValue(big) // 改副本，原对象不变\nbySlice(s)   // 改共享数组，调用方可见" :notes='["结构体按值传参：整个 Big 被复制一份，函数内改的是副本","切片按值传参：只复制 24 字节的头，Data 仍指向同一底层数组","调用 byValue 后外部 big 不受影响，这是值语义的直接结果","调用 bySlice 后外部 s 被改写，因为两个头指向同一份数据"]' />

## 3. 深入一步

要谈「拷贝成本」，先得知道每种值到底多大。`unsafe.Sizeof` 返回类型在内存里占的字节数，是编译期常量，不触发运行时分配，是安全的观察手段。

```go
package main

import (
	"fmt"
	"unsafe"
)

func main() {
	var i int
	var s string
	var sl []int
	var m map[string]int
	var e any
	var st struct{ A, B, C int }
	fmt.Println("int        ", unsafe.Sizeof(i))                // 8
	fmt.Println("string     ", unsafe.Sizeof(s))                // 16：Data 指针 + Len
	fmt.Println("[]int      ", unsafe.Sizeof(sl))               // 24：Data + Len + Cap
	fmt.Println("map        ", unsafe.Sizeof(m))                // 8：一个 *hmap 指针
	fmt.Println("interface  ", unsafe.Sizeof(e))                // 16：类型字 + 数据字
	fmt.Println("struct 3int", unsafe.Sizeof(st))               // 24：字段之和
	fmt.Println("[1024]int  ", unsafe.Sizeof([1024]int{}))      // 8192：真·大对象
}
```

```bash
# 直接跑，观察 64 位机器上的头大小
go run main.go
# 换架构可以看到指针宽度变化（32 位下指针为 4 字节）
GOARCH=386 go build main.go
# 想知道某个局部变量是否被搬到堆，用下一讲的逃逸分析
go build -gcflags=-m main.go
```

把上面的输出整理成表，规律就清楚了：

| 类型 | 值里装的是什么 | 64 位下大小 | 赋值后是否共享底层数据 |
| --- | --- | --- | --- |
| `int` / `bool` | 数据本身 | 8 / 1 字节 | 否，完全独立 |
| `[1024]int` | 数据本身 | 8192 字节 | 否，整份复制 |
| `struct{A,B,C int}` | 数据本身 | 24 字节 | 否，整份复制 |
| `string` | `Data` 指针 + `Len` | 16 字节 | 是（且不可变） |
| `[]T` | `Data` + `Len` + `Cap` | 24 字节 | 是，除非触发扩容 |
| `map` / `chan` / `func` | 单个指针 | 8 字节 | 是 |
| `interface` | 类型字 + 数据字 | 16 字节 | 取决于装箱的值 |

<Term word="描述符（header / descriptor）" text="只记录「数据在哪、有多长」的小结构，真正的数据在别处。拷贝它很廉价，但多个描述符可能指向同一份数据，构成共享。" />

由此得到一条工程判断：结构体字段越多、数组越大，一次赋值要搬的字节就越多；而切片、map、字符串无论存了多少元素，赋值永远只搬那十几字节的头。

<Callout type="warn" title="别把「共享」当成 bug，也别当成理所当然" text="切片、map 赋值后共享底层数据是设计使然，能省掉大块拷贝；但只要一方触发扩容（slice）或重新 make（map），共享关系就会断开。判断依据永远是「值里装的是数据还是指针」。第 28、29 讲会把这两处断开讲透。" />

还要补一个常被忽略的版本因素：**编译器优化会改写成本结论**。从 Go 1.17 起，函数参数在 amd64 等平台上改用基于寄存器的调用约定（register ABI），多个小字段可以直接走寄存器，比旧的栈上逐字段拷贝便宜；编译器还可能做拷贝消除与内联，把一次「看起来很贵」的传值整个优化掉。所以「传值一定慢」是错的，反过来「传指针一定快」也错——取地址往往让对象逃逸到堆，反而多一次分配与 GC 压力，这正是下一讲的主题。语义上一切仍是值拷贝，优化只改变实现，不改变可观察结果。

### 推导链：值拷贝在编译器里到底是什么

「一切皆值」不是口号，而是编译器对赋值与调用的固定降级方式。以 `cmd/compile/internal/walk` 与 `cmd/compile/internal/ssagen` 为例：源码里的 `b := a` 在 IR 阶段被展开成 `a` 到 `b` 的逐字段复制；函数传参在 Go 1.17 起的寄存器 ABI 下由 `cmd/compile/internal/abi` 决定，小结构体被拆成若干寄存器或栈槽位逐个传递，而不是传一个隐藏引用。所以「为什么 `byValue` 改不到原对象」的答案就落在编译产物里：被调函数拿到的是独立栈槽位或寄存器副本，地址与调用方不同。把第 2 节那段 `byValue` / `bySlice` 程序存成 `vs.go`，可用两条命令看到这条链：

```bash
go build -gcflags=-S -o /dev/null vs.go 2>&1 | grep -E 'MOV|LEA|CALL'
go build -gcflags=-m vs.go 2>&1 | grep -E 'escape|does not escape'
# 若因构建缓存没有输出，先 go clean -cache，或给命令追加 -a 强制重编
```

<OutputBox title="反汇编片段（amd64 示意 / Go 1.23，行号与寄存器名随平台变化）" output="0x0018 00024 MOVQ AX, (SP)           // 字段 X 复制到被调栈帧\n0x001c 00028 MOVQ BX, 8(SP)          // 字段 Y 复制\n0x0020 00032 LEAQ go:string.*(SB), CX // Tag 的 Data 指针复制\n0x0028 00040 CALL main.byValue(SB)" note="amd64 示意：每行把 a 的字段搬进新栈位置；arm64 下为 MOVD 与 R 系列寄存器，但同样没有「引用」被登记或传递。" />

逐字段解释：示意里的 `MOVQ`/`LEAQ`（arm64 为 `MOVD`）复制的是字段值本身，`CALL` 之后被调函数操作的已是另一块栈空间；`go build -gcflags=-m` 里若出现 `a does not escape`，还说明这次拷贝连堆都没上。把「值里装数据」换成「值里装描述符」，复制的只是 `Data`、`Len`、`Cap` 三个字——`unsafe.Sizeof` 量的正是这三种字的宽度。

**为什么这样设计**：默认引用传递会让任何函数都能悄悄改到调用方的数据，别名分析、常量传播与并发正确性都会失控；Go 选值拷贝换可预测性，再用「描述符加共享底层数据」把大块内存的搬运成本压到常数级。替代方案（由程序员自己选值、引用或移动，如 C++）表达力更强，但心智负担、悬垂指针与接口一致性成本也更高。代价是结构体字段越多拷贝越贵，于是第 27 讲的逃逸分析与本讲的 `unsafe.Sizeof` 成了必配的量化手段。

<OutputBox title="平台字长对照（size.go 输出）" output="GOARCH=amd64：IntSize=64  指针=8  string=16  []int=24  Desc=24\nGOARCH=386 ：IntSize=32  指针=4  string=8   []int=12  Desc=12" note="同一份代码，32 位下所有描述符都缩小一半；unsafe.Sizeof 是编译期常量，可通过交叉编译观察。" />

```go
package main

import (
	"fmt"
	"runtime"
	"strconv"
	"unsafe"
)

type Desc struct {
	ptr unsafe.Pointer
	len int
	cap int
}

func main() {
	fmt.Println("GOARCH:", runtime.GOARCH, " IntSize:", strconv.IntSize)
	fmt.Println("指针:", unsafe.Sizeof(uintptr(0)))
	fmt.Println("string:", unsafe.Sizeof(""))
	fmt.Println("[]int:", unsafe.Sizeof([]int{}))
	fmt.Println("Desc:", unsafe.Sizeof(Desc{}))
}
```

命令：`go run size.go` 看本机结果，`GOARCH=386 go build size.go` 交叉编译确认 32 位布局。逐字段解释：`IntSize` 是 `int` 与 `uintptr` 的位宽，决定 `Len`、`Cap` 占 4 还是 8 字节；`string` 是 `Data` 加 `Len` 两个字；`[]int` 是 `Data` 加 `Len` 加 `Cap` 三个字；`Desc` 手写镜像验证「描述符大小等于一个指针加两个 int」。注意 `unsafe.Sizeof` 只返回头部字节数，**不含底层数组**——一个百万元素的切片 `Sizeof` 仍是 24，这是它作为观测手段最容易被误读的局限。

### 寄存器 ABI 下的一次传值：字段拆到哪、何时落栈

上一节用反汇编示意看到「逐字段搬运」，但没回答「搬到哪」。Go 1.17 起的寄存器 ABI 把这件事变成一次可计算的分配：输入 `func f(s S) int`，`cmd/compile/internal/abi` 按字段逐个调用 `AssignStackOrReg` 分配参数位置——先填满整数参数寄存器，放不下的字段落到栈参数区（FP 正偏移）。字段状态因此分成两类：`s.A` 在 R0、`s.B` 在 R1、`s.C` 在 R2；而 `s.V[k]` 在 `main.s+8k(FP)`。分支条件就是「字段序号是否小于 `IntArgRegs`」。可观察输出是 `go tool compile -S` 里的 `TEXT ... $0-N`（N 为参数区字节数）与具体指令：寄存器版直接 `ADD R0, R1, R1`，栈版则是 `MOVD main.s+128(FP), R2`。

用两个只差字段数的结构体把边界量出来：

```go
package main

type S3 struct{ A, B, C int }

type S20 struct{ V [20]int }

//go:noinline
func f3(s S3) int { return s.A + s.B + s.C }

//go:noinline
func f20(s S20) int { return s.V[0] + s.V[9] + s.V[16] + s.V[19] }

func main() { println(f3(S3{1, 2, 3}), f20(S20{})) }
```

```bash
# 用 go tool compile -S 直通编译器，避开 go build 的构建缓存吞掉汇编输出
go tool compile -S abi.go 2>&1 | grep -A9 'TEXT.*main.f3'
go tool compile -S abi.go 2>&1 | grep -A12 'TEXT.*main.f20'
```

<OutputBox title="go tool compile -S 片段（Go 1.23 / arm64）" output="TEXT main.f3(SB), LEAF|NOFRAME|ABIInternal, $0-24\n  ADD R0, R1, R1\n  ADD R1, R2, R0\n  RET (R30)\n--- f20 ---\nTEXT main.f20(SB), LEAF|NOFRAME|ABIInternal, $0-160\n  MOVD main.s(FP), R1\n  MOVD main.s+72(FP), R2\n  MOVD main.s+128(FP), R2\n  MOVD main.s+152(FP), R2" note="f3 三个字段全在寄存器；f20 从 FP 正偏移的栈参数区取字段。" />

逐字段解释：`$0-24` 表示栈帧 0 字节、参数区 24 字节，正好是 3 个字段乘 8；三个 `ADD` 直接吃 R0、R1、R2，说明 3 个字段全落在寄存器，函数连栈帧都不用（`LEAF|NOFRAME`）。`$0-160` 则是 20 字段乘 8 的参数区；`main.s(FP)` 与 `main.s+72/128/152(FP)` 分别对应 `V[0]`、`V[9]`、`V[16]`、`V[19]`，偏移就是「字段下标乘 8」，说明这些字段从**栈参数区**读取而非寄存器。量化依据在 `internal/abi/abi_*.go` 的 `IntArgRegs`：arm64 与 loong64、riscv64 是 16，amd64 是 9，ppc64x 是 12；所以同一个 `S20` 在 amd64 上溢到栈的字段比 arm64 更多。

**反直觉结论的证伪**：「传大结构体一定触发堆分配」与「字段越多拷贝成本线性增长」都不成立。对照里 `f20` 仍是 `LEAF|NOFRAME`、参数区 160 字节全在栈上，用 `-gcflags=-m` 复核没有任何 `moved to heap`，即 0 次堆分配；成本曲线是分段的——`IntArgRegs` 以内的字段只占寄存器、几乎不产生搬运指令，超出部分才落进栈参数区。这也补上了「为什么语义上是整份拷贝、实测却不慢」的另一半：真正决定是否上堆的是第 27 讲的逃逸分析（取地址、返回指针、装箱），而不是字段数或结构体大小。

边界：结论依赖 Go 1.17+ 的寄存器 ABI，更早版本全走栈且参数位置不同；`IntArgRegs` 是平台相关的硬常量，换 `GOARCH` 必须重算分界；数组、string、slice、interface 等字段的拆分规则与纯整数不同（例如接口是两个字）；`//go:noinline` 是本实验前提，一旦被内联，整个调用与参数区都会被优化掉；32 位平台字长 4 字节，所有偏移与参数区字节数减半；`-gcflags=-S` / `go tool compile -S` 的寄存器名随平台变化，amd64 下是 AX、BX、CX 系列。

### 两套寄存器堆与 ABI0 包装：浮点、接口参数究竟落在哪

前面把 `IntArgRegs` 当成一条单一分界线，其实寄存器 ABI 有**两套彼此独立的寄存器堆**。`internal/abi/abi.go` 的 `RegArgs` 把它们写得很清楚：

```text
// 摘自 Go 1.23.8 的 internal/abi/abi.go（仅摘录字段）
type RegArgs struct {
    Ints   [IntArgRegs]uintptr  // 整数/指针寄存器堆
    Floats [FloatArgRegs]uint64 // 浮点寄存器堆，独立计数
    Ptrs   [IntArgRegs]unsafe.Pointer
    ReturnIsPtr IntArgRegBitmap
}
```

`IntArgRegs` 与 `FloatArgRegs` 是平台常量：`abi_arm64.go` 为 `16 / 16`，`abi_amd64.go` 为 `9 / 15`。参数在编译期由 `cmd/compile/internal/abi/abiutils.go` 的 `ABIConfig.ABIAnalyzeTypes` 逐组件调用 `assignState.assignParam`（旧资料常写的 `AssignStackOrReg` 是 Go 1.17 时代的名字，1.23 已重构为 `assignParam`）决定去向：整数、指针、接口、字符串、切片这些字进 `Ints`，浮点字段进 `Floats`，**两套堆各自从零计数、互不挤占**。一个接口是两个字（类型字 + 数据字），因此固定吃掉 2 个整数寄存器；一个 `float64` 吃掉 1 个浮点寄存器、0 个整数寄存器。分界条件因此不是一条，而是两条：`整数寄存器用量 > IntArgRegs` 或 `浮点寄存器用量 > FloatArgRegs` 时，放不下的组件才落到栈参数区 `FP` 正偏移。

代入一个可算的例子：`func f(i0..i15 int, x any)` 在 arm64（`IntArgRegs=16`）上，16 个 int 正好填满 R0..R15，`x` 的两个字需要 R16、R17 而越界，于是 `x` 整体落到栈参数区；同样的函数在 amd64（`IntArgRegs=9`）上，第 10 个 int 就开始溢出。反过来 `func g(a int, b float64)` 在 arm64 上是 `a → R0`、`b → F0`，`b` 完全不占用 R1——「字段序号小于 `IntArgRegs`」这条旧直觉只适用于整数堆。

Go 1.17 之前所有参数都按栈 ABI 放在 `FP` 正偏移；1.17 引入的是**双 ABI**：普通 Go 调 Go 走 `ABIInternal`（寄存器），汇编函数、`//go:linkname`、反射调用与 cgo 仍走栈式 `ABI0`。编译器于是为需要的函数额外生成 `.abi0` 包装符号，把寄存器里的实参「溢出」回栈参数区再跳转。`go tool nm` 里成片的 `runtime.*.abi0` 就是这条兼容层的产物，也是「寄存器 ABI 不是把栈 ABI 删掉、而是叠加一层」的可查证据。

新观察角度：不用编译期汇编、直接反汇编**链接后的二进制**，看寄存器堆里的浮点与接口实参怎么被消费。把下面程序存成 `abi.go`（`//go:noinline` 是前提，否则整个调用会被内联掉）：

```go
package main

//go:noinline
func f(a int, b float64, c any) float64 {
	_ = c
	return float64(a) + b
}

func main() { println(f(1, 2, 3)) }
```

```bash
go build -o abi_bin abi.go
go tool objdump -s '^main\.f' ./abi_bin    # 链接产物，带源码行号
GOARCH=amd64 go build -o abi_amd abi.go
go tool objdump -s '^main\.f' ./abi_amd    # 交叉编译看另一套寄存器名
# 兼容层证据：统计 .abi0 包装符号
go tool nm ./abi_bin | grep -c '\.abi0'
# 常量出处：直接读本机 GOROOT 的 abi 源文件
sed -n '20,32p' "$(go env GOROOT)/src/internal/abi/abi.go"
sed -n '9,16p' "$(go env GOROOT)/src/internal/abi/abi_arm64.go"
```

<OutputBox title="go tool objdump 片段（Go 1.23.8 / darwin arm64）" output="TEXT main.f(SB) /tmp/abi.go\n  abi.go:4  0x100066660  9e620001  SCVTFD R0, F1\n  abi.go:4  0x100066664  1e602820  FADDD F0, F1, F0\n  abi.go:4  0x100066668  d65f03c0  RET\n--- GOARCH=amd64 交叉对照 ---\nTEXT main.f(SB) /tmp/abi.go\n  abi.go:4  0x1065d00  0f57c9      XORPS X1, X1\n  abi.go:4  0x1065d03  f2480f2ac8  CVTSI2SDQ AX, X1\n  abi.go:4  0x1065d08  f20f58c1    ADDSD X1, X0\n  abi.go:4  0x1065d0c  c3          RET\n--- go tool nm 计数 ---\n若干 runtime.*.abi0 包装符号" note="arm64 用 F/D 系列浮点寄存器，amd64 用 X 系列；两版都把 a 与 b 放进不同寄存器堆。" />

逐字段解释：`SCVTFD R0, F1` 把整数参数 `a` 所在的 R0 转成双精度写进 F1，`FADDD F0, F1, F0` 里 F0 正是 `b` 的落点——两个操作数分别来自整数堆与浮点堆，证明 `b` 没有挤占 R1；amd64 版 `CVTSI2SDQ AX, X1` 读 AX（a）、`ADDSD X1, X0` 用 X0（b），同样是两套堆。`go tool nm | grep -c '\.abi0'` 得到的计数不是固定值，随构建链接进来的 runtime 函数变化，但它非零就说明 ABI0 兼容层确实被生成。`sed` 两行给出 `RegArgs.Ints/Floats` 字段与 `IntArgRegs = 16`、`FloatArgRegs = 16`，把「16 / 16」这组代入值从口算变成源码可查。

边界：`FloatArgRegs` 与 `IntArgRegs` 都是平台硬常量，换 `GOARCH` 必须重算两条分界；结构体里的浮点字段同样走浮点堆，含接口字段则按 2 个字计；`go tool objdump` 读的是链接后的机器码，寄存器名与地址随平台和构建变化，只比指令语义、不比数值；`//go:noinline` 一旦去掉，函数体被铺进 `main` 就没有独立 TEXT 段可反汇编；`.abi0` 包装只给「需要被汇编/反射/cgo 以栈 ABI 调用」的函数生成，别把「某函数没有 .abi0」当成「它不走寄存器」。这条链在第 30 讲（接口两字结构）会解释 `iface` 为什么恰好占 2 个整数寄存器，第 36 讲（SSA `regalloc`）会讲寄存器分配的最后一公里，第 37 讲（cgo 跨边界）则是 ABI0 仍存活的现实场景。

### 边界、常见误解与前后联系

| 常见误解 | 正解 | 可验证依据 |
| --- | --- | --- |
| Go 的 slice / map 是「引用类型」，赋值是引用传递 | Go 没有任何引用传递；赋值的永远是值，只是描述符里装了指针才共享 | 反汇编里是三个字的搬运，无引用登记；第 2 节 `s2 := s1` 的实验 |
| `unsafe.Sizeof(s)` 给出切片或字符串占用的全部内存 | 只给头部字节数，底层数组另算 | 上面的平台对照表；大容量切片的 `Sizeof` 恒为 24 |
| 传指针一定比传值快（含 32 位平台） | 32 位下指针只有 4 字节、拷贝更便宜；且取地址常引发逃逸与 GC 压力 | `GOARCH=386` 结果；`go build -gcflags=-m` 看 `moved to heap` |
| 共享关系一旦建立就永久有效 | 切片扩容、map 重新 `make` 会断开共享 | 第 28、29 讲 |

边界要点：并发下两个 goroutine 无同步地读写同一份共享底层数组是数据竞争（`-race` 能抓，第 49 讲）；`nil` 描述符头部全零，可安全 `len` 与 `append`；平台字长改变所有头部大小；`unsafe.Sizeof` 与 `-m` 都是编译期或静态观察，看不到运行时扩容与 GC 行为，必须叠加 `go test -benchmem` 与 `GODEBUG=gctrace=1` 才完整。

前后联系：第 27 讲把「取地址导致逃逸」接着讲透，第 28 至 31 讲逐个拆开四类描述符的头部；本讲「值拷贝在 ABI 上如何落地」会在第 98 讲（编译器源码：SSA、逃逸与内联）从 IR 与寄存器分配的角度重讲一遍，而「为什么共享内存必须靠同步」则是第 99 讲（Go 内存模型与 happens-before）的形式化前提。做性能决策时，第 100 讲（极致性能优化）把「先量化拷贝成本、再决定是否改指针」作为闭环第一步。

## 4. 工程实践

大对象该传值还是传指针？先量化，再决定。下面这段用 `//go:noinline` 阻止内联消除拷贝，用累加变量 `sink` 防止整个循环被优化掉，让差异可以复现。

```go
package main

import (
	"fmt"
	"time"
)

type Big struct{ data [1024]int }

//go:noinline
func consume(b Big) int { return b.data[0] + b.data[1023] }

//go:noinline
func consumePtr(b *Big) int { return b.data[0] + b.data[1023] }

func main() {
	var big Big
	const N = 5_000_000
	var sink int
	start := time.Now()
	for i := 0; i < N; i++ {
		sink += consume(big)
	}
	dv := time.Since(start)
	start = time.Now()
	for i := 0; i < N; i++ {
		sink += consumePtr(&big)
	}
	dp := time.Since(start)
	fmt.Printf("传值 %v，传指针 %v，sink=%d\n", dv, dp, sink)
}
```

<Compare :panels='[{"label":"传值","code":"func consume(b Big) int","note":"调用时拷贝整个 Big，约 8192 字节"},{"label":"传指针","code":"func consume(b *Big) int","note":"只拷贝 8 字节指针，但可能让对象逃逸到堆"}]' title="大对象传参的两种写法" />

几条可直接落地的经验：读多写少、需要避免被调用方意外修改时，值语义更安全；对象大、或确需共享修改时传指针。但别凭感觉——用 `-gcflags=-m` 看是否逃逸、用 `go test -bench=. -benchmem` 看分配次数，让数据说话。另外，热路径里把大结构体反复按值传参，比多写几个字段访问更容易被忽略；把它缩成指针或只传需要的字段，往往比纠结接收者类型更划算。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l3-core/lesson-026-value-semantics`](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-026-value-semantics)。clone 仓库后进入 `examples/l3-core`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="用 Sizeof 找出最贵的拷贝" task="定义一个含 [512]int 字段的结构体，用 unsafe.Sizeof 分别打印它与它的指针大小；再写一个函数分别以值和指针接收它，用你的话说明两者各拷贝多少字节。" :hints='["unsafe.Sizeof 返回 uintptr，是编译期常量","指针在 64 位机器上恒为 8 字节"]' solution="type Big struct { data [512]int }\nfmt.Println(unsafe.Sizeof(Big{}))  // 4096\nfmt.Println(unsafe.Sizeof(&Big{})) // 8" explain="值传递一次搬 4096 字节，指针只搬 8 字节；但取地址可能引发逃逸，最终成本要结合 -gcflags=-m 与基准测试一起判断。" />

## 随堂检测

<Quiz no="26-1" q="s := []int{1,2,3}; t := s 之后立刻执行 t[0] = 9，此时 s[0] 是多少？" :options='["1","9","不确定","编译错误"]' answer="1" explain="赋值只复制切片头，t 与 s 的 Data 指向同一底层数组，改 t[0] 等于改同一块内存。只有之后发生 append 扩容，两者才可能分家。" />

<Predict no="26-2" q="b := a（a 是结构体）之后执行 b.X = 100，a.X 会怎样？" :options='["变成 100","保持原值","变成 0","编译错误"]' answer="1" output="保持原来的值" explain="结构体赋值是整份值拷贝，b 是独立副本，改 b 不影响 a。这正是「值语义」最朴素的表现。" />

## 一页纸速记

<KnowledgeCard :points='["Go 一切赋值与传参默认按值拷贝，没有「引用传递」","切片 / 字符串 / map / 接口 / chan / func 的值是描述符，拷贝廉价但共享底层数据","数组与大结构体是真·整份拷贝，用 unsafe.Sizeof 量化成本","编译器会做拷贝消除、内联与寄存器传参（Go 1.17+ register ABI），但语义仍是值拷贝"]' :pitfalls='["误以为切片赋值是深拷贝，改了一个影响另一个","认为传指针一定更快：它可能引发逃逸，反而多一次堆分配","忘记共享关系会因扩容或重新 make 而断开"]' summary="先看清「值」里装的是数据本身还是描述符，再谈拷贝成本。" />

## 小结与下一讲

- 一切皆值：`=` 和函数传参都是拷贝，区别只在拷贝的是整份数据还是描述符。
- 描述符类型（切片、字符串、map、接口）只有十几字节，赋值廉价但会共享底层数据。
- 拷贝成本要用 `unsafe.Sizeof`、`-gcflags=-m` 和基准测试衡量，别凭直觉认为指针一定更快。

既然「取地址可能让对象逃逸到堆」，那编译器究竟依据什么决定变量住在栈还是堆？下一讲我们来读懂逃逸分析的输出。

[下一讲：逃逸分析与编译器决策](/core/escape-analysis)
