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
