---
title: 第 31 讲 · 字符串底层与零拷贝转换
description: 知道字符串转换的代价与安全边界：string 头部结构、不可变与共享、[]byte 转换拷贝、unsafe 零拷贝及风险
---

<StageHeader stage="L3 · 核心机制" level="★★★★☆" time="35 分钟" goal="知道字符串转换的代价与安全边界" prereq="第 08 讲 · 字符串、rune 与高效拼接" />

# 第 31 讲 · 字符串底层与零拷贝转换

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

<Callout type="warn" title="unsafe 零拷贝的风险边界" text="它换掉的不是性能，是不变性契约：一，改写源 []byte 会穿透到字符串，让把它当 map 键、缓存键或比较基准的代码出错；二，字符串字面量常驻只读内存段，强转成可写切片再赋值可能直接 SIGSEGV；三，必须保证底层对象在指针使用期间存活，不能用 uintptr 跨语句保存（第 37 讲展开）；四，这类行为不是语言保证，跨编译器版本可能变化。" />

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

## 动手练一练

<Exercise title="证明标准转换会拷贝" task="写程序把字符串转成 []byte，比较 unsafe.StringData(s) 与 unsafe.SliceData(b) 是否相同，再修改字节切片，确认原字符串不变。" :hints='["两个函数都返回字节指针，可直接比较是否相等","修改 b[0] 后再打印 s 即可验证"]' solution="b := []byte(s)\nfmt.Println(unsafe.StringData(s) != unsafe.SliceData(b))\nb[0] = 88\nfmt.Println(s)" explain="打印 true 说明是两块内存；改 b 后 s 不变，证明标准转换发生了拷贝。" />

## 随堂检测

<Quiz no="31-1" q="执行 b := []byte(s); b[0] = 88 之后，原字符串 s 会怎样？" :options='["首字符被改写","完全不变","编译错误","取决于 s 是否为常量"]' answer="1" explain="[]byte(s) 会分配新数组并拷贝字节，b 是独立副本；改 b 不影响 s，这正是不可变性的保证。" />

<Predict no="31-2" q="用 unsafe.String 把 []byte 零拷贝转成 string 后，再修改原 []byte，字符串内容会怎样？" :options='["保持不变","跟着一起变","panic","编译错误"]' answer="1" output="跟着一起变：字符串内容被改写，违反不可变约定" explain="零拷贝让两者共享同一块内存，写 []byte 会穿透到 string。依赖不可变的代码（map 键、缓存、比较）可能因此出错。" />

<FillBlank no="31-3" tpl="Go 1.20 起，unsafe 包提供了 unsafe.String 与 unsafe.___ 用于零拷贝构造字符串、取字符串数据指针；标准转换 string(b) 则一定会发生一次 ___。" :answers='["StringData","拷贝"]' hint="填一个函数名与一个动作" explain="unsafe.StringData 取字符串数据指针；标准的 []byte 与 string 互转会分配新数组并拷贝字节。" />

## 一页纸速记

<KnowledgeCard :points='["string 头是 Data 指针 + Len 两字（16 字节），因不可变而不需要 Cap","子串共享底层字节；len 数的是字节，range 按 rune 解码","[]byte(s) 与 string(b) 默认都拷贝；Go 1.20 起 unsafe.String/StringData 可零拷贝","零拷贝只适用于只读、短生命周期、已量化的热点，且必须保证源不被改写"]' :pitfalls='["零拷贝后修改源 []byte，破坏不可变导致隐蔽 bug","把字符串强转成可写切片，可能写只读段而崩溃","依赖编译器省略拷贝来写逻辑，跨版本即失效"]' summary="string 的不可变是语义契约，unsafe 零拷贝是拿安全性换一次分配，代价必须先想清楚。" />

## 小结与下一讲

- 字符串头只有 `Data` 指针 + `Len`，不可变让它无需 `Cap`，也让子串共享安全。
- 标准 `string` / `[]byte` 互转必然拷贝；Go 1.20 起 `unsafe` 提供零拷贝，但改写源数据会破坏不可变性。
- 零拷贝只该出现在只读、短生命周期、经 `benchmem` 确认的热点，并配套注释与测试。

到这里，值语义、逃逸、以及切片 / map / interface / 字符串四类描述符的底层都拆过了。它们省下的每一次分配最终由谁提供？下一讲进入内存分配器，看 `mcache / mcentral / mheap` 如何把堆内存送到你手上。

[下一讲：内存分配器：mcache / mcentral / mheap](/core/allocator)
