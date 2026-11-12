---
title: 第 031 讲 · 字符串底层与零拷贝转换
description: 知道字符串转换的代价与安全边界：string 头部结构、不可变与共享、[]byte 转换拷贝、unsafe 零拷贝及风险
---

<StageHeader stage="L3 · 核心机制" level="★★★★☆" time="35 分钟" goal="知道字符串转换的代价与安全边界" prereq="第 008 讲 · 字符串、rune 与高效拼接" />

# 第 031 讲 · 字符串底层与零拷贝转换

> 字符串看起来最朴素，却是本阶段前半的收尾关键：它和切片一样是「小头 + 指向别处」，却多了一条铁律——**不可变**。正因不可变，子串才能安全地共享内存，也正因不可变，`string` 与 `[]byte` 每次互转都得老老实实拷贝一遍。想用 `unsafe` 省掉这次拷贝？先把代价算清楚。

## 这一讲你会得到什么

<CheckList id="goal-31" title="本讲目标" :items='["能画出 string 头的两个字段并解释为何没有 Cap","理解子串共享、len 按字节、range 按 rune 的底层原因","证明标准转换会发生拷贝，并知道编译器何时可能省略","说清 unsafe 零拷贝的收益与全部风险边界"]' />

## 1. 先建立直觉

把 `string` 头想成切片头去掉了 `Cap`：只剩「数据在哪」的 `Data` 指针和「有多少字节」的 `Len`。为什么不需要容量？因为字符串不可变，永远不会有 `append` 往后面追加，也就用不着预留空间。下图切片头的三个框，去掉 `Cap` 那一格，就是字符串头。

<ConceptDiagram type="slice-header" title="string 头：Data + Len（比切片头少 Cap）" caption="因为不可变，无需容量字段；多个子串可安全共享同一段字节" />

## 2. 语法与规则

先用镜像结构量一下头部，并确认不可变是编译期就钉死的。

```go
package main

import (
	"fmt"
	"unsafe"
)

// stringHeader 与运行时 string 头一致：数据指针 + 字节长度
type stringHeader struct {
	Data unsafe.Pointer
	Len  int
}

func main() {
	s := "hello, 世界"
	h := (*stringHeader)(unsafe.Pointer(&s))
	fmt.Println("string 头字节数:", unsafe.Sizeof(s)) // 16
	fmt.Println("字节长度 len(s):", len(s))           // 字节数，不是字符数
	fmt.Println("头里的 Len:", h.Len)                 // 与 len(s) 相同
	fmt.Printf("Data 指向: %p, StringData: %p\n", h.Data, unsafe.StringData(s))
	// s[0] = 'H' // 取消注释会编译失败：string 不可变
}
```

<LineByLine code="type stringHeader struct { Data unsafe.Pointer; Len int }\nh := (*stringHeader)(unsafe.Pointer(&s))\nfmt.Println(h.Len, unsafe.StringData(s))" :notes='["字符串头只有两个字：数据指针 + 字节长度，64 位下共 16 字节","把字符串变量地址重解释成头，直接读字段","unsafe.StringData 是 Go 1.20 起官方提供的取数据指针方式"]' />

正因为不可变，切子串可以放心共享同一段字节，不必拷贝；而 `len` 数的是字节，`range` 才按 UTF-8 解码成 rune。

```go
package main

import (
	"fmt"
	"unsafe"
)

func main() {
	s := "Go语言"
	sub := s[:2] // 子串共享同一块底层字节，不拷贝
	fmt.Printf("s   Data=%p\n", unsafe.StringData(s))
	fmt.Printf("sub Data=%p\n", unsafe.StringData(sub)) // 相同地址
	fmt.Println("字节数:", len(s), " rune 数:", len([]rune(s)))
	for i, r := range s {
		fmt.Printf("字节偏移 %d 对应 %c\n", i, r)
	}
}
```

<OutputBox title="运行结果（地址每次运行不同）" output="s   Data=0x4c8f20\nsub Data=0x4c8f20\n字节数: 8  rune 数: 4\n字节偏移 0 对应 G\n字节偏移 1 对应 o\n字节偏移 2 对应 语\n字节偏移 5 对应 言" />

`Go语言` 共 8 字节、4 个 rune：两个 ASCII 各占 1 字节，两个汉字各占 3 字节，所以偏移跳到 2、5。`sub` 与 `s` 地址相同，说明切子串只是换了头的 `Len`，没有动数据——这正是不变性带来的免费午餐。

## 3. 深入一步

一旦要和 `[]byte` 互转，不变性就要求「写隔离」：标准转换必须分配新数组并拷贝，否则改字节就会改到字符串。

```go
package main

import (
	"fmt"
	"unsafe"
)

func main() {
	s := "immutable"
	b := []byte(s) // 转换：分配新数组并拷贝
	fmt.Printf("s Data=%p, b Data=%p\n", unsafe.StringData(s), unsafe.SliceData(b))
	b[0] = 'I' // 只改副本
	fmt.Println("s 仍是:", s)
	fmt.Println("b 变成:", string(b)) // 又一次拷贝

	s2 := string(b) // []byte 转 string 同样拷贝
	b[0] = 'X'
	fmt.Println("s2 不受影响:", s2)
}
```

| 转换方式 | 是否拷贝 | 安全性 | 适用场景 |
| --- | --- | --- | --- |
| `[]byte(s)` / `string(b)` | 是 | 安全，维持不可变 | 默认选择 |
| 编译器优化（比较、作 map 键） | 可能省略 | 安全 | 只读临时值，属实现细节 |
| `unsafe.String` / `unsafe.StringData` | 否 | 有风险 | 只读、短生命周期、已量化收益 |

<Term word="零拷贝（zero-copy）" text="复用同一块底层字节，不做新分配与数据复制。省掉一次分配与拷贝，代价是双方共享内存，任何一方改写都会影响另一方。" />

这里有一处**版本差异**要记：编译器在能证明安全时会省略临时拷贝，例如 `m[string(b)]` 用字节切片当 map 键、`string(b) == "x"` 只做比较时，可能根本不分配。但这种优化随版本演进、且只在很窄的模式下生效，**不能拿来写业务逻辑**。另外，Go 1.20 才正式加入 `unsafe.String`、`unsafe.StringData`、`unsafe.SliceData`，让零拷贝有了官方（但仍属 unsafe）途径；更早的代码只能用 `reflect.StringHeader` 之类的镜像，风险更高。

<Callout type="warn" title="unsafe 零拷贝的风险边界" text="它换掉的不是性能，是不变性契约：一，改写源 []byte 会穿透到字符串，让把它当 map 键、缓存键或比较基准的代码出错；二，字符串字面量常驻只读内存段，强转成可写切片再赋值可能直接 SIGSEGV；三，必须保证底层对象在指针使用期间存活，不能用 uintptr 跨语句保存（第 037 讲展开）；四，这类行为不是语言保证，跨编译器版本可能变化。" />

### 推导链：从 runtime/string.go 推到「必须拷贝」与「优化省略」

结论的出处是 `runtime/string.go`：内部头 `stringStruct` 只有 `str unsafe.Pointer` 与 `len int` 两个字段（没有 `Cap`，因为不可变永远不需要为追加预留空间）；`slicebytetostring`、`stringtoslicebyte`、`rawstring`、`rawstringtmp`、`concatstrings` 负责转换与拼接。`cmd/compile/internal/walk` 在编译期把 `m[string(b)]`、`string(b) == "x"` 这类只读临时用法降级为带栈上临时缓冲的 `slicebytetostringtmp`，从而省略分配。四条推导链：

- **子串共享**：`s[i:j]` 只改 `stringStruct.len`、复用同一个 `str`，所以 `unsafe.StringData(s)` 与 `unsafe.StringData(sub)` 相等；不可变让这种共享永远安全。
- **标准转换必须拷贝**：`[]byte(s)` 走 `stringtoslicebyte` 分配新数组并拷贝，否则写字节会改到字符串；反过来 `string(b)` 走 `slicebytetostring` 同样拷贝。这是不变性的直接代价，不是实现偷懒。
- **编译器省略拷贝**：当转换结果只用于 map 键查找或相等比较且不逃逸时，`cmd/compile/internal/walk` 用栈上的 `tmpBuf` 承接短字符串，产物里不再出现 `slicebytetostring` 调用（旧版本常见 `slicebytetostringtmp`），也就没有 `mallocgc`。
- **零拷贝**：`unsafe.String`/`StringData`（Go 1.20 起）只是把头指向源字节，不分配、不拷贝，等于把不变性契约交给调用方自己守。

把下面程序存成 `conv.go`，用分配次数与汇编同时验证「拷贝」与「省略」：

```go
package main

import (
	"fmt"
	"testing"
)

var sink string

//go:noinline
func convReturn(b []byte) string { return string(b) } // 结果外泄，必须拷贝

//go:noinline
func convKey(m map[string]int, b []byte) int { return m[string(b)] } // 只读临时，可省略

func main() {
	b := []byte("key")
	m := map[string]int{"key": 7}
	a := testing.AllocsPerRun(1000, func() { sink = convReturn(b) })
	c := testing.AllocsPerRun(1000, func() { _ = convKey(m, b) })
	fmt.Printf("返回式约 %.0f allocs/op，临时键约 %.0f allocs/op\n", a, c)
}
```

```bash
go run conv.go
go build -gcflags=-S -o /dev/null conv.go 2>&1 | grep -E 'slicebytetostring|rawstring|mallocgc'
# 若因构建缓存没有输出，先 go clean -cache 或追加 -a
```

<OutputBox title="conv.go 实测与汇编关键词（Go 1.23 / arm64）" output="返回式约 1 allocs/op，临时键约 0 allocs/op\n--- 汇编关键词 ---\nconvReturn: CALL runtime.slicebytetostring(SB)\nconvKey   : 无 string 转换调用（栈上临时缓冲直接用于查找）" note="同一份源码，外泄与不外泄走了两条不同的路径。" />

逐字段解释：`convReturn` 的结果要活着离开函数，只能走 `slicebytetostring`（内部 `rawstring` → `mallocgc` → `memmove`），故约 1 次分配；`convKey` 的结果只当 map 键、不逃逸，编译器把临时字符串直接构造在栈上的缓冲里交给 map 查找，连 `slicebytetostring` 调用都不出现，故 0 次分配（旧版本这里可能表现为 `slicebytetostringtmp`）。注意这是**编译器优化细节、随 Go 版本变化**，`AllocsPerRun` 数值也会因内联与版本波动，只能作为同一版本内的对比证据，绝不能写进业务逻辑。

**为什么这样设计**：不可变让「子串共享」「可作 map 键」「并发只读无需同步」「字面量进只读段」同时成立；若可变，任一处改写都要 COW 或加锁，且 map 键的哈希会被破坏，代价比一次拷贝高得多。正因如此标准转换必须拷贝；Go 再用「编译器在只读临时场景省略拷贝」与「`unsafe` 显式零拷贝」两个口子对冲，但两者都把「守住不变量」的责任交给程序员。替代方案（可变字符串加引用计数）在频繁改写场景更省，却会拖累所有只读路径，与 Go 的服务端负载特征不符。

### tmpBuf 的 32 字节边界：省略拷贝到底覆不覆盖长字符串

上一节测了 `convReturn`（结果外泄，1 次分配）与 `convKey`（只当 map 键，0 次分配），但没回答「0 次分配有没有长度上限」。看 `runtime/string.go`：`tmpStringBufSize = 32`，`type tmpBuf [32]byte` 是编译器在调用点预留的栈缓冲。当 `string(b)` 这类临时转换不逃逸时，编译器在栈上放一个 `tmpBuf`，`slicebytetostring(buf, ...)` / `rawstringtmp(buf, l)` 在 `l <= 32` 时直接把字节填进缓冲、返回一个指向栈的 string 头，于是零堆分配；`l > 32` 时 buf 传 nil，退回 `rawstring` 里的 `mallocgc` 堆分配。分支条件就是 `l <= len(buf)`。可观察输出是 `testing.AllocsPerRun` 在长度 32 与 33 之间的跳变。

用「必须真的构造一个临时 string」的场景把这条件钉死：

```go
package main

import (
	"fmt"
	"testing"
)

var sinkInt int

//go:noinline
func use(s string) int { return len(s) }

//go:noinline
func callUse(b []byte) int { return use(string(b)) }

func main() {
	for _, n := range []int{8, 16, 31, 32, 33, 64} {
		b := make([]byte, n)
		for i := range b {
			b[i] = 'a'
		}
		a := testing.AllocsPerRun(1000, func() { sinkInt = callUse(b) })
		fmt.Printf("string(b) 长度 %2d B: %.0f allocs/op\n", n, a)
	}
}
```

```bash
go run tmpbuf.go
# 对照：map 键查找走的是另一条路径，与 tmpBuf 无关
go build -gcflags=-S -o /dev/null tmpbuf.go 2>&1 | grep -E 'slicebytetostring|mapaccess1_faststr' || true
```

<OutputBox title="tmpBuf 阈值实测（Go 1.23 / arm64）" output="string(b) 长度  8 B: 0 allocs/op\nstring(b) 长度 16 B: 0 allocs/op\nstring(b) 长度 31 B: 0 allocs/op\nstring(b) 长度 32 B: 0 allocs/op\nstring(b) 长度 33 B: 1 allocs/op\nstring(b) 长度 64 B: 1 allocs/op\n--- go build -gcflags=-S ---\nCALL runtime.slicebytetostring(SB)\nCALL main.use(SB)" note="32 与 33 之间一步跳变，阈值正是 tmpBuf 的 32 字节；长路径汇编里可见 slicebytetostring。" />

逐字段解释：`8/16/31/32` 都满足 `l <= 32`，编译器把临时 string 构造在调用点的栈缓冲里，`0 allocs/op`；`33/64` 越界，产物里出现 `CALL runtime.slicebytetostring(SB)`，每次 1 次堆分配。注意 `use` 与 `callUse` 都标了 `//go:noinline`：否则编译器可能把整个调用链优化掉，连转换都不发生。这里的 32 不是随便取的——`tmpBuf` 是 `[32]byte`，一次机器字对齐的栈上小缓冲，短字符串用它换掉一次 `mallocgc` 加 `memmove`。

**反直觉结论的证伪**：「编译器省略临时拷贝是普适优化，任意长度都省」是错的，长于 32 字节就退回堆分配，所以「接收大网络包转成 string 当 key」不能指望编译器兜底。更细的一条是「上一节 `convKey` 的 0 次分配靠 tmpBuf」也错：把上一节那个 `convKey(m, b)` 的 `b` 换成 3、16、32、33、64 字节，实测**全都**是 0 次分配——`go build -gcflags=-S` 显示它被降级为 `CALL runtime.mapaccess1_faststr`，编译器直接把 `b` 的字节指针与长度当字符串键传给 map 查找，根本不构造 string，因此与长度、与 tmpBuf 都无关。两种「零分配」来自两条完全不同的路径，别混为一谈：一条是栈缓冲承接短字符串，一条是 map 键查找免去构造。

边界：`tmpStringBufSize = 32` 是 Go 1.23 的常量，随版本可能调整；编译器省略拷贝属优化细节，跨版本、跨写法（比较、拼接、作参数）生效模式不同，绝不能写进业务逻辑；`//go:noinline` 与 `AllocsPerRun` 的数值都受内联决策影响；Go 1.20 起才有 `unsafe.String`/`StringData` 这套官方零拷贝途径；32 位平台栈布局不同但阈值不变；`unsafe` 零拷贝的风险边界仍以本节之前的警告为准。

### 多操作数拼接与比较省略：编译器把 `+` 降级成了哪几个函数

前两节已经量过 `tmpBuf` 的 32 字节阈值、也分清了「栈缓冲承接短字符串」与「map 键免去构造」两条零分配路径，这一节不重复测阈值，改看另外两个一直没展开的分支：**操作数个数怎样决定调用哪个运行时函数**，以及 **`string(b) == 常量` 究竟省掉了什么**。依据都在 `runtime/string.go`（Go 1.23.8）。

**拼接的元数分派**：`runtime/string.go` 里有 `concatstrings(buf *tmpBuf, a []string) string` 一个可变参数主体，外加 `concatstring2`、`concatstring3`、`concatstring4`、`concatstring5` 四个固定元数的薄包装，每个形如 `return concatstrings(buf, []string{a0, a1, ...})`。编译器（`cmd/compile/internal/walk` 与 `ssagen`）按 `+` 的操作数个数选入口：2 到 5 个操作数分别调 `concatstring2..5`，**6 个及以上才直接调 `concatstrings`** 并把操作数收成一个 `[]string` 描述符传进去。分派条件是「编译期可见的操作数个数」，不是「字符串长度」。固定元数版本的收益是参数可以直接走 ABI 寄存器/调用点栈槽，包装函数内部那张固定长度切片能被编译器整块留在栈上；到了 6 元以上，调用方就得真的构造一个切片头再加若干字，帧会变大。

`concatstrings` 内部还有一处常被忽略的**拷贝省略**：

```text
第一趟  累加非空操作数的长度 l，记非空个数 count 与最后一个下标 idx
        count == 0 → 直接返回 ""
第二趟  若 count == 1 且 (buf 非空 或 该操作数不在栈上) → 直接返回 a[idx]
        否则 s, b := rawstringtmp(buf, l)
             for _, x := range a { copy(b, x); b = b[len(x):] }
```

也就是说 `"" + s`、`s + ""` 这类「只有一个非空操作数」的拼接根本不分配、不拷贝，原样返回那个 string 头；`buf` 参数就是调用点预留的栈缓冲，非空时 `rawstringtmp` 用它承接结果、不进堆。这与第 3 节「编译器在只读临时场景省略拷贝」是同一套 `tmpBuf` 机制，但触发点是 `count == 1`，不是长度阈值。

**比较的拷贝省略**：`string(b) == "abc"` 里那个 `string(b)` 压根不会被构造。编译器把它降级成「先比长度、再按字节比内容」：短常量串直接展开成几条 `CMP`/`MOV` 指令，不调用 `slicebytetostring`、也不调用 `runtime.memequal`；而 `a == b` 两个运行时长度的字符串则退化成一次 `CALL runtime.memequal(SB)`。这是上一节表格里「编译器优化（比较）可能省略」那一行真正的机器码形态。

新观察角度：反汇编**链接后的二进制**，把元数分派与比较降级一起看。存成 `cat.go`：

```go
package main

//go:noinline
func cat3(a, b, c string) string { return a + b + c }

//go:noinline
func cat6(a, b, c, d, e, f string) string { return a + b + c + d + e + f }

//go:noinline
func eqConst(b []byte) bool { return string(b) == "abc" }

//go:noinline
func eqVar(a, b string) bool { return a == b }

func main() {
	println(cat3("a", "b", "c"), cat6("a", "b", "c", "d", "e", "f"),
		eqConst([]byte("abc")), eqVar("a", "a"))
}
```

```bash
go build -o catbin cat.go
go tool objdump -s '^main\.(cat3|cat6|eqConst|eqVar)' ./catbin | grep -E 'TEXT main|concatstring|memequal|CALL|MOVD \$|CMP|CSET'
sed -n '25,58p' "$(go env GOROOT)/src/runtime/string.go"
```

<OutputBox title="拼接元数与比较降级的机器码（Go 1.23.8 / arm64）" output="TEXT main.cat3(SB)\n  cat.go:4  CALL runtime.concatstring3(SB)\nTEXT main.cat6(SB)\n  cat.go:7  CALL runtime.concatstrings(SB)\nTEXT main.eqConst(SB)\n  cat.go:10  CMP $3, R1\n  cat.go:10  MOVHU (R0), R1\n  cat.go:10  MOVD $25185, R2\n  cat.go:10  CMPW R2, R1\n  cat.go:10  MOVBU 2(R0), R1\n  cat.go:10  CMPW $99, R1\n  cat.go:10  CSET EQ, R1\nTEXT main.eqVar(SB)\n  cat.go:13  CALL runtime.memequal(SB)" note="3 元与 6 元走不同入口；常量比较被展开成 8 条指令，无任何运行时调用。" />

逐字段解释：`cat3` 只有 `CALL runtime.concatstring3(SB)`，证明 3 个操作数被固定元数入口接走；`cat6` 变成 `CALL runtime.concatstrings(SB)`，且函数开头有 `SUB $16, RSP, R17` 与 144 字节栈帧，说明 6 个操作数要先在调用点铺成一个 `[]string`。`eqConst` 的 `CMP $3, R1` 先比长度（`"abc"` 长 3）；`MOVD $25185, R2` 里的 `25185 = 0x6261` 正是 ASCII `"ab"`，`MOVHU (R0), R1` 一次读两个字节与之比；`CMPW $99` 比第三个字节 `'c'`；`CSET EQ` 收束结果——全程既没有 `slicebytetostring` 构造字符串，也没有 `memequal` 调用，这就是「拷贝省略」最具体的形态。对照 `eqVar` 的 `CALL runtime.memequal(SB)`，可见省略只在「一侧是编译期常量、长度已知」时成立。`sed -n '25,58p'` 给出的正是 `concatstrings` 两趟扫描、`count==1` 直接返回与 `rawstringtmp` 那几行源码，把「为什么没有拷贝」对上。

量化一层：常量比较从「一次 `slicebytetostring`（可能分配 + `memmove`）加一次 `memequal`」降到 8 条整数/加载指令、0 次运行时调用、0 次分配；代价是这段展开只对编译期已知长度的常量有效，常量越长展开的指令越多，编译器会在超过一定长度时改回 `memequal`。拼接侧则是 `concatstring2..5` 免掉「调用方构造 `[]string` 描述符」，6 元以上多付一个切片头的构造与更大的栈帧。工程含义与第 4 节一致：热路径里把 `string(b) == 常量` 交给编译器是安全的，但别指望它对动态长度的两个字符串也生效；拼接大量字符串时，用 `strings.Builder` 一次写完，比让编译器按元数分派、反复 `copy` 更可控。

边界：`concatstring2..5` 的名称、`tmpBuf`、`rawstringtmp` 都是 Go 1.23 的实现细节，跨版本先 `go version` 再对照 `runtime/string.go`；「6 个操作数」是本机实测分派点，随编译器改写可能变化；`go tool objdump` 的寄存器名与立即数编码随 GOARCH 变化（amd64 下 `25185` 会换成别的取字节方式），只比「有无运行时调用」；`//go:noinline` 是本实验前提，否则 `cat3`、`eqConst` 会被内联进 `main` 而失去独立 TEXT 段；`count == 1` 的省略还受 `stringDataOnStack` 判定约束，操作数在栈上时不会直接返回。这条链在第 036 讲（编译期降级与 SSA）与第 098 讲（walk 阶段）会再展开。

### 边界、常见误解与前后联系

| 常见误解 | 正解 | 可验证依据 |
| --- | --- | --- |
| `string(b)` 一定会分配 | 只作 map 键或相等比较等只读临时值时，编译器可能用栈缓冲省略拷贝 | 上面的 `conv.go` 与 `go build -gcflags=-S` |
| `len(s)` 是字符数 | 是字节数；一个汉字 3 字节，`range` 才按 UTF-8 解码成 rune | 第 2 节 `Go语言` 的字节偏移输出 |
| `unsafe.String` 只是快一点，其它一样 | 它放弃不可变契约，写源 `[]byte` 会改写字符串，字面量只读段还可能直接崩 | 第 4 节 `bytesToString` 的穿透实验 |

边界要点：32 位下字符串头 8 字节；空字符串 `""` 的 `Data` 常指向运行时 `zerobase`；`unsafe.String(&b[0], len(b))` 在 `b` 为空时会 panic（不能对空切片取 `&b[0]`），应用 `unsafe.SliceData`；字符串字面量常在只读段，强转成可写切片后写入会 SIGSEGV；Go 1.20 才有 `unsafe.String`/`StringData`/`SliceData`，更早的 `reflect.StringHeader` 镜像风险更高；编译器省略拷贝是优化、随版本变化；并发下标准转换安全，`unsafe` 零拷贝因共享可写内存而不安全；`unsafe.Sizeof` 只测头部、`unsafe.StringData` 的返回值不能被长期保存成整数。

前后联系：第 008 讲（字符串与拼接）、第 026 讲（描述符）与第 028 讲（切片头）在这里收束；第 098 讲（编译器源码：SSA、逃逸与内联）会看 `slicebytetostring` 系列在 `cmd/compile/internal/walk` 与 SSA 阶段如何被省略、以及逃逸分析怎样决定用不用 `tmpBuf`，第 100 讲（极致性能优化）把「先 profile 再决定零拷贝」作为典型案例，第 099 讲（Go 内存模型）说明只读共享为何无需同步、而 `unsafe` 结果为何必须自己保证 happens-before。

## 4. 工程实践

真要用零拷贝，先看它到底有多危险：下面程序里，改 `b` 直接改掉了由它转出的字符串。

```go
package main

import (
	"fmt"
	"unsafe"
)

// bytesToString 零拷贝：复用 b 的底层数组，不分配
func bytesToString(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// stringToBytes 零拷贝：复用 s 的字节，返回的切片不可写
func stringToBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func main() {
	b := []byte("zero-copy")
	s := bytesToString(b) // s 与 b 共享内存
	fmt.Println("转换结果:", s)
	b[0] = 'Z' // 写 b 会「穿透」到 s
	fmt.Println("改写 b 后 s:", s) // 违反不可变，输出 Z 开头

	ro := stringToBytes("read-only")
	fmt.Println("只读切片:", string(ro))
	// ro[0] = 'X' // 运行时可能崩溃：字符串常在只读段
}
```

<Compare :panels='[{"label":"标准转换","code":"s := string(b)","note":"分配新数组并拷贝，安全且符合不可变约定"},{"label":"unsafe 零拷贝","code":"s := unsafe.String(unsafe.SliceData(b), len(b))","note":"不分配，但要求 b 此后绝不被修改"}]' title="[]byte 转 string 的两种写法" />

给工程决策划条线：默认永远用标准转换；接收网络字节流转成字符串当 map 键时，先信编译器的拷贝消除，别急着手写 `unsafe`；只有在大对象、高频、且 `go test -benchmem` 明确显示 `allocs/op` 是瓶颈时，才考虑零拷贝。真用了，就必须把「源数据此后只读、生命周期覆盖字符串」写成注释、用测试锁死，并把转换收进一个小函数里，避免 `unsafe` 散落各处。记住：省下的是一次分配，赌上的是全程序对「字符串不可变」的假设。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l3-core/lesson-031-string-internals`](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-031-string-internals)。clone 仓库后进入 `examples/l3-core`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="证明标准转换会拷贝" task="写程序把字符串转成 []byte，比较 unsafe.StringData(s) 与 unsafe.SliceData(b) 是否相同，再修改字节切片，确认原字符串不变。" :hints='["两个函数都返回字节指针，可直接比较是否相等","修改 b[0] 后再打印 s 即可验证"]' solution="b := []byte(s)\nfmt.Println(unsafe.StringData(s) != unsafe.SliceData(b))\nb[0] = 88\nfmt.Println(s)" explain="打印 true 说明是两块内存；改 b 后 s 不变，证明标准转换发生了拷贝。" />

## 随堂检测

<Quiz no="031-1" q="执行 b := []byte(s); b[0] = 88 之后，原字符串 s 会怎样？" :options='["首字符被改写","完全不变","编译错误","取决于 s 是否为常量"]' answer="1" explain="[]byte(s) 会分配新数组并拷贝字节，b 是独立副本；改 b 不影响 s，这正是不可变性的保证。" />

<Predict no="031-2" q="用 unsafe.String 把 []byte 零拷贝转成 string 后，再修改原 []byte，字符串内容会怎样？" :options='["保持不变","跟着一起变","panic","编译错误"]' answer="1" output="跟着一起变：字符串内容被改写，违反不可变约定" explain="零拷贝让两者共享同一块内存，写 []byte 会穿透到 string。依赖不可变的代码（map 键、缓存、比较）可能因此出错。" />

<FillBlank no="031-3" tpl="Go 1.20 起，unsafe 包提供了 unsafe.String 与 unsafe.___ 用于零拷贝构造字符串、取字符串数据指针；标准转换 string(b) 则一定会发生一次 ___。" :answers='["StringData","拷贝"]' hint="填一个函数名与一个动作" explain="unsafe.StringData 取字符串数据指针；标准的 []byte 与 string 互转会分配新数组并拷贝字节。" :explains='["该函数返回字符串底层字节的起始指针，与 unsafe.String 配合零拷贝构造；生命周期与不可变性须自行保证。","字符串是不可变值，而字节切片可写，两者语义冲突，所以标准转换必须新分配一段数组并复制全部字节，不能共享同一块底层内存。"]' />

## 一页纸速记

<KnowledgeCard :points='["string 头是 Data 指针 + Len 两字（16 字节），因不可变而不需要 Cap","子串共享底层字节；len 数的是字节，range 按 rune 解码","[]byte(s) 与 string(b) 默认都拷贝；Go 1.20 起 unsafe.String/StringData 可零拷贝","零拷贝只适用于只读、短生命周期、已量化的热点，且必须保证源不被改写"]' :pitfalls='["零拷贝后修改源 []byte，破坏不可变导致隐蔽 bug","把字符串强转成可写切片，可能写只读段而崩溃","依赖编译器省略拷贝来写逻辑，跨版本即失效"]' summary="string 的不可变是语义契约，unsafe 零拷贝是拿安全性换一次分配，代价必须先想清楚。" />

## 小结与下一讲

- 字符串头只有 `Data` 指针 + `Len`，不可变让它无需 `Cap`，也让子串共享安全。
- 标准 `string` / `[]byte` 互转必然拷贝；Go 1.20 起 `unsafe` 提供零拷贝，但改写源数据会破坏不可变性。
- 零拷贝只该出现在只读、短生命周期、经 `benchmem` 确认的热点，并配套注释与测试。

到这里，值语义、逃逸、以及切片 / map / interface / 字符串四类描述符的底层都拆过了。它们省下的每一次分配最终由谁提供？下一讲进入内存分配器，看 `mcache / mcentral / mheap` 如何把堆内存送到你手上。

[下一讲：内存分配器：mcache / mcentral / mheap](/core/allocator)
