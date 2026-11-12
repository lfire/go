---
title: 第 13 讲 · 数组与切片：定义与操作
description: 数组是值、切片是描述符，掌握 len/cap、append、copy 与切片表达式
---

<StageHeader stage="L2 · 语法核心" level="★★☆☆☆" time="35 分钟" goal="熟练使用 Go 最常用的序列结构" prereq="第 12 讲 · 阶段综合：CLI 单位换算器" />

# 第 13 讲 · 数组与切片：定义与操作

> 程序总在处理「一串数据」。Go 给了两种载体：长度写死的数组，和长度可变的切片。为什么日常代码几乎只用切片？因为数组赋值、传参都要整块搬走，切片只带一张轻量「取货单」。学完本讲，你能选对容器、读懂 len 与 cap，并避开共享底层数组的隐性改动。

## 这一讲你会得到什么
<CheckList id="l13-goals" title="本讲目标" :items='["分清数组是值、切片是描述符","会用字面量、make 与切片表达式","读懂 len/cap，用对 append 与 copy","识别共享底层数组并主动隔离"]' />

## 1. 先建立直觉

数组像一排焊死的储物格：数量刻进类型，`[3]int` 与 `[4]int` 互不兼容；交给别人只能连格带内容整箱复制。切片像取货单，只记三个数：起点 ptr、可见长度 len、从起点到仓库末尾的可用格数 cap。货物始终在底层数组里。复制单据极便宜，但两张单据指向同一仓库时，改一处另一处也会看见。

这也解释了 Go 的一个取舍：数组把长度交给类型系统，换来编译期检查；切片把长度交给运行时，换来灵活，却要求你自己留意共享。二者不是替代关系而是分工——切片底层仍然靠数组存元素，只是把「有多少格」从类型里挪到了运行时。

<ConceptDiagram type="slice-header" title="描述符与底层数组" caption="len 是可见长度，cap 是不扩容就能用到的上限" />

<Term word="底层数组" text="真正存放元素的连续内存，可被多个切片同时指向。" />

## 2. 语法与规则

下面先验证数组是值：长度属于类型，赋值与传参都是整份拷贝。

```go
package main

import "fmt"

func main() {
	var a [3]int = [3]int{1, 2, 3} // 长度写在类型里
	b := a                          // 整份拷贝
	b[0] = 99
	fmt.Println("a =", a, "b =", b)
	mutate(a)
	fmt.Println("mutate 后 a =", a)
}

func mutate(x [3]int) {
	x[1] = 77 // 只改副本
	fmt.Println("mutate 内 x =", x)
}
```

输出里 `b[0]` 变了而 `a` 不变；`mutate` 改的也只是副本。这正是数组很少当参数的原因——大数组拷贝成本高。

切片有三种创建方式：字面量、切片表达式、`make`。第三个参数是预分配容量，不改可见长度，只决定 `append` 扩容前能塞多少元素。

```go
package main

import "fmt"

func main() {
	s1 := []int{10, 20, 30} // len 与 cap 都等于 3
	s2 := make([]int, 3, 8) // 预分配，减少 append 扩容
	fmt.Println("s1:", s1, len(s1), cap(s1))
	fmt.Println("s2:", s2, len(s2), cap(s2))

	arr := [5]int{0, 1, 2, 3, 4}
	part := arr[1:4] // 左闭右开，取下标 1、2、3
	fmt.Println("part:", part, len(part), cap(part))
}
```

<OutputBox title="运行结果" output="s1: [10 20 30] 3 3\ns2: [0 0 0] 3 8\npart: [1 2 3] 3 4" />

<LineByLine code="arr := [5]int{0, 1, 2, 3, 4}\npart := arr[1:4]\nfmt.Println(len(part), cap(part))" :notes='["长度为 5 的数组，长度属于类型","左闭右开取下标 1、2、3，len 为 3","cap 从起点数到数组末尾，所以是 4"]' title="切片表达式逐行拆解" />

注意 `s2` 打印出来是三个 0：`make` 会把已分配的元素初始化为零值，`len` 决定你能直接看到几个，`cap` 只影响后续增长。这三者的分工，是读懂后面所有切片行为的关键。

`part` 的 cap 是 4 而非 3：cap 从切片起点算到数组末尾，起点下标 1、数组长 5，还剩 4 格。它决定下一次 `append` 是否就地写入。

## 3. 深入一步

| 维度 | 数组 `[N]T` | 切片 `[]T` |
| --- | --- | --- |
| 长度 | 属于类型，编译期固定 | 运行时可变 |
| 赋值传参 | 整块拷贝 | 只拷贝描述符 |
| 零值 | 元素全为零，可直接用 | `nil`，len/cap 为 0 |
| 比较 | 元素可比较时整体可比 | 只能与 `nil` 比 |
| 用途 | 定长缓冲、哈希、矩阵 | 绝大多数动态序列 |

为什么切片不把元素直接装在变量里？因为那样每次传参都要拷贝全部元素，代价随长度线性增长。描述符方案让传参成本恒定，代价是引入了共享：编译器不再帮你隔离数据，判断责任落到写代码的人身上。底层如何扩容、何时换数组属于 L3 的内容，本讲先掌握使用层能观察到的现象与判断方法。

<Callout type="warn" title="共享底层数组" text="子切片与原切片共用存储；append 未扩容时会改写原数组。需要隔离就用 copy 或 make 重建。" />

<StepLab scene="slice-grow" />

下面一次跑出共享、隔离与扩容分家三种现象。`left` 有余量，`append` 直接改写 `base`；`copy` 搬进新数组后互不影响；`full` 容量已满，`append` 另起炉灶。

```go
package main

import "fmt"

func main() {
	base := []int{1, 2, 3, 4}
	left := base[0:2]
	left = append(left, 99) // 就地写入 base[2]
	fmt.Println("append 后 base =", base)

	buf := make([]int, len(base))
	n := copy(buf, base) // 复制到独立数组
	buf[0] = 7
	fmt.Println("copy", n, "个；buf =", buf, "base =", base)

	full := []int{3, 4}
	grown := append(full, 88) // 容量满，分配新数组
	grown[0] = 30
	fmt.Println("full =", full, "grown =", grown)
}
```

<OutputBox title="运行结果" output="append 后 base = [1 2 99 4]\ncopy 4 个；buf = [7 2 99 4] base = [1 2 99 4]\nfull = [3 4] grown = [30 4 88]" />

## 4. 工程实践

三条习惯值得记住。能预估规模就预分配，`make([]T, 0, len(src))` 让循环内 `append` 全程不扩容；返回子切片会钉住整个底层数组，原数组很大而只需少量元素时先 `copy` 出来；`append` 的结果一定要接住并传回所有持有方，扩容后两个切片从此分家。

<Compare title="批量构造切片" :panels='[{"label":"写法 A","code":"var out []int\nfor _, v := range src {\n    out = append(out, v)\n}","note":"容量反复增长，多次分配拷贝"},{"label":"写法 B","code":"out := make([]int, 0, len(src))\nfor _, v := range src {\n    out = append(out, v)\n}","note":"一次给足容量，不再扩容"}]' />

<Callout type="tip" title="nil 切片可直接用" text="var s []int 的 len 与 cap 都是 0，可安全 append 与 range；只有 nil map 需要先 make。" />

把镜头再推近一层，先回答「描述符到底便宜在哪」。切片变量在 64 位机器上只占三个机器字：指向底层数组的指针、`len`、`cap`，用 `unsafe.Sizeof([]int{})` 量出来恒为 24 字节，和里面装了多少元素毫无关系；`[1024]int` 却是 8192 字节，赋值或传参一次就是整块 8KB 拷贝。这就是「数组是值、切片是描述符」在机器层面的依据，也解释了标准库里几乎看不到拿大数组当参数的函数。写十几行 `main` 打印两个 `unsafe.Sizeof`，再对传数组的函数跑 `go build -gcflags=-m`，输出里的 `escapes to heap` 就是逃逸分析留下的现场证据。

再说版本差异。Go 1.21 才把 `slices` 包收进标准库，带来 `Clone`、`Contains`、`Sort` 等一批泛型函数；在此之前，克隆切片得自己 `make` 加 `copy`。所以读老项目先翻 `go.mod` 里的 `go` 指令：`slices` 包要求 1.21 及以上。`slices.Clone` 还顺手固定了 nil 语义——传 nil 得 nil，传非 nil 的空切片得非 nil 的空切片，比手写 `make` 更不容易混淆「nil」与「空」。至于 `append` 究竟何时换数组，由 `runtime/slice.go` 里的 `growslice` 决定，新容量怎么算属于 L3 话题；本讲只需记住那条使用层结论：因为它可能返回一块全新数组，`append` 的结果必须接住。

真实踩坑案例来自日志和网络缓冲：有人写 `return buf[:n]`，把一个只用到前 64 字节的子切片返回出去，而 `buf` 是从池里取出的 1MB 大数组。子切片仍引用整块底层数组，GC 便不敢回收那 1MB，长生命周期地挂着这个返回值就等于把整块内存钉住。修法很朴素：`small := make([]byte, n); copy(small, buf[:n]); return small`，用一次拷贝换回可回收。可以用 `runtime.ReadMemStats` 前后对比 `HeapInuse` 验证：隔离前反复调用内存居高不下，隔离后明显回落。

<ConceptDiagram type="slice-grow" title="append 的两条分支" caption="cap 够用时就地写回共享数组，不够时分配新数组并复制，返回新的描述符" />

下面两段代码把上面几条边界落到可运行的形式。第一段演示 `copy` 会静默截断、越界要显式报错、以及 nil 输入如何安全返回。

```go
package main

import (
	"errors"
	"fmt"
)

// clonePrefix 复制 src 的前 n 个元素，返回互不影响的独立切片。
// 三个真实边界：n 越界要报错、copy 可能静默截断、nil 输入要安全返回。
func clonePrefix(src []int, n int) ([]int, error) {
	if n < 0 || n > len(src) {
		return nil, fmt.Errorf("前缀长度 %d 超出 0~%d", n, len(src))
	}
	if src == nil {
		return nil, nil // nil 进，nil 出，调用方 range/append 都安全
	}
	out := make([]int, n)
	if copied := copy(out, src[:n]); copied != n {
		return nil, errors.New("复制数量不足")
	}
	return out, nil
}

func main() {
	ok, err := clonePrefix([]int{1, 2, 3}, 2)
	fmt.Println("克隆 =", ok, "出错 =", err != nil)
	ok[0] = 99
	fmt.Println("改克隆 =", ok)

	_, err = clonePrefix([]int{1}, 5)
	fmt.Println("越界提示 =", err)

	empty, err := clonePrefix(nil, 0)
	fmt.Println("nil 输入得到 =", empty, "出错 =", err != nil, "append 后 =", append(empty, 7))

	// copy 只搬 min(len(dst), len(src)) 个，dst 偏小会静默截断
	small := make([]int, 2)
	n := copy(small, []int{10, 20, 30})
	fmt.Println("只搬了", n, "个 =", small)
}
```

<OutputBox title="运行结果" output="克隆 = [1 2] 出错 = false\n改克隆 = [99 2]\n越界提示 = 前缀长度 5 超出 0~1\nnil 输入得到 = [] 出错 = false append 后 = [7]\n只搬了 2 个 = [10 20]" />

第二段对比 Go 1.21 才进标准库的 `slices` 包：用 `Clone` 与 `Contains` 替掉手写循环，并确认它对 nil 与空切片的处理。

```go
package main

import (
	"fmt"
	"slices"
)

func main() {
	// nil 切片
	var nilSlice []int
	// 非 nil 的空切片
	emptySlice := make([]int, 0)

	c1 := slices.Clone(nilSlice)
	c2 := slices.Clone(emptySlice)
	fmt.Println("克隆 nil 后仍是 nil =", c1 == nil)
	fmt.Println("克隆空切片后仍是 nil =", c2 == nil)

	src := []int{1, 2, 3}
	c3 := slices.Clone(src)
	c3[0] = 99 // 改副本，原切片不受影响
	fmt.Println("c3 =", c3, "src =", src)
	fmt.Println("包含 2 =", slices.Contains(src, 2))
}
```

<OutputBox title="运行结果" output="克隆 nil 后仍是 nil = true\n克隆空切片后仍是 nil = false\nc3 = [99 2 3] src = [1 2 3]\n包含 2 = true" />

| 观察项 | nil 切片 `var s []int` | 空切片 `make([]int, 0)` |
| --- | --- | --- |
| `len(s)` / `cap(s)` | 0 / 0 | 0 / 0 |
| `s == nil` | true | false |
| 直接 `range s` | 安全，循环零次 | 安全，循环零次 |
| 直接 `append(s, 1)` | 安全，会分配 | 安全，会分配 |
| `slices.Clone(s) == nil` | true | false |

<Predict no="13-3" q="dst := make([]int, 2) 后 n := copy(dst, []int{10,20,30})，打印 n 与 dst 的结果是？" :options='["3 [10 20 30]","2 [10 20]","2 [10 20 0]","编译失败"]' answer="1" output="2 [10 20]" explain="copy 只搬 min(len(dst), len(src)) 个元素，dst 只有两格，第三个数被静默丢弃；所以要检查返回值、别假设全量复制。" />

## 动手练一练

<Exercise title="安全截取前缀" task="由 src := []int{1, 2, 3, 4, 5} 造出长度 3、内容相同但互不影响的 dst，改 dst[0] 为 9 后打印两者。" :hints='["别用 src[0:3]，仍共享数组","make 分配新切片，再 copy"]' solution="dst := make([]int, 3)\ncopy(dst, src)\ndst[0] = 9\nfmt.Println(src, dst)" explain="make 分配独立数组，copy 只搬元素，改 dst 不回写 src。输出 [1 2 3 4 5] [9 2 3]。" />

## 随堂检测

<Quiz no="13-1" q="关于数组与切片，正确的是？" :options='["数组赋值只复制描述符","s[1:3] 的 cap 一定是 2","长度属于类型，[3]int 与 [4]int 不能互赋","nil 切片必须先 make 才能 append"]' answer="2" explain="数组是值，整块拷贝，长度不同即类型不同；cap 从起点算到数组末尾；nil 切片可直接 append。" />

<Predict no="13-2" q="arr 长度为 5，part := arr[1:4] 后打印 len(part), cap(part) 的结果是？" :options='["3 3","3 4","4 4","2 4"]' answer="1" output="3 4" explain="左闭右开取 3 个元素；cap 从下标 1 数到末尾共 4 格。" />

## 一页纸速记

<KnowledgeCard :points='["数组是值，切片是描述符","len 是可见长度，cap 决定 append 是否扩容","切片表达式左闭右开","append 可能就地改写共享数组，copy 才隔离"]' :pitfalls='["以为改子切片不影响原切片","append 后没接住结果","把 nil 切片规则套到 nil map"]' summary="数组管定长，切片管动态；切片背后总有一块可能被共享的数组。" />

## 小结与下一讲

- 数组把长度写进类型，是「值」；切片只带描述符，是「视图」。
- `len` 是可见长度，`cap` 决定扩容；`make` 第三参数可预分配。
- 共享底层数组会互相看见修改；用 `copy` 隔离，`append` 结果必须接住。

有了容器，下一步是让程序按条件选择、按次数重复。

[下一讲：控制流：if / for / switch / goto](/basics/control-flow)
