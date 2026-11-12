---
title: 第 18 讲 · 方法与接收者：值还是指针
description: 理解方法集，分清值与指针接收者，写出不踩坑的接收者设计
---

<StageHeader stage="L2 · 语法核心" level="★★☆☆☆" time="30 分钟" goal="理解方法集，写出不踩坑的接收者" prereq="第 17 讲 · 结构体、标签与内存对齐" />

# 第 18 讲 · 方法与接收者：值还是指针

> 结构体把数据装好了，接下来要让它有行为：计数、校验、序列化。Go 没有类，但可以给类型绑定函数，调用时写成 `x.F()`，读起来和面向对象一样自然。真正需要想清楚的是那个写在 `func` 后面的接收者：用值还是用指针？这个选择决定方法能不能改到原值，也决定类型能不能满足某个接口。学完本讲，你能给出有依据的判断，而不是凭感觉挑一个。

## 这一讲你会得到什么
<CheckList id="l18-goals" title="本讲目标" :items='["会定义方法","分清两种接收者","用方法集判断接口实现","处理 nil 接收者"]' />

## 1. 先建立直觉

把方法想成「绑定到某个类型上的普通函数」：接收者其实就是隐式的第一个参数。既然是参数，值接收者拿到的就是一份拷贝，改它动不到原件；指针接收者拿到的是地址，改的是同一份数据。Go 在调用侧帮你自动做取地址和解引用，所以 `c.Add()` 与 `(&c).Add()` 常常都能写，但语义差别藏在方法内部。

<ConceptDiagram type="iface" title="接收者决定方法集" labels="值接收者::T 与 *T 可调|指针接收者::只有 *T 可调|接口实现::看方法集|nil 接收者::可为 nil" caption="方法集决定接口实现" />

<FlowTimeline title="一次方法调用" :steps='[{"title":"写调用","desc":"c.Add(1)","tone":"blue"},{"title":"取地址","desc":"自动转换","tone":"orange"},{"title":"进方法","desc":"改副本或原值","tone":"green"},{"title":"返回","desc":"结果留在 c 上","tone":"purple"}]' direction="horizontal" />

## 2. 语法与规则

方法定义只比函数多一个接收者声明，写在 `func` 与方法名之间。下面这段把两种接收者放在同一个类型上对比：值接收者改的是副本，指针接收者改到原值；反过来，指针变量也能调用值接收者方法，编译器会自动解引用。

```go
package main

import "fmt"

type Counter struct {
	N int
}

// 值接收者：拿到副本
func (c Counter) AddValue(delta int) {
	c.N += delta
}

// 指针接收者：改到原值
func (c *Counter) Add(delta int) {
	c.N += delta
}

func main() {
	c := Counter{N: 1}
	c.AddValue(10)
	fmt.Println("值接收者后 N =", c.N)
	c.Add(10)
	fmt.Println("指针接收者后 N =", c.N)

	p := &Counter{N: 1}
	p.AddValue(5) // 指针也能调值方法
	fmt.Println("指针调值方法后 N =", p.N)
}
```

<OutputBox title="运行结果" output="值接收者后 N = 1\n指针接收者后 N = 11\n指针调值方法后 N = 1" />

<LineByLine code="func (c *Counter) Add(d int) {\n    c.N += d\n}\nc.Add(1)" :notes='["接收者写在 func 后的括号里","指针接收者改到原始结构体","方法体到此结束","值变量调用时编译器自动取地址"]' title="方法定义逐行拆解" />

<Term word="方法集" text="类型可调用的方法集合；T 只含值接收者方法，*T 含两类。" />

## 3. 深入一步

方法集的意义在接口实现时显形：接口要求的方法必须完整出现在赋给它的那个动态类型的方法集里。因此用值接收者实现的方法，`T` 与 `*T` 都满足接口；只用指针接收者实现的方法，只有 `*T` 满足，`T` 不满足。编译期写一行 `var _ Interface = (*T)(nil)` 是常用的断言习惯。

```go
package main

import "fmt"

type Speaker interface {
	Speak() string
}

type Dog struct{}

func (d Dog) Speak() string { return "wang" }

type Cat struct{}

func (c *Cat) Speak() string { return "miao" }

func main() {
	var s Speaker
	s = Dog{} // 值类型即满足接口
	fmt.Println(s.Speak())

	s = &Cat{} // 只有指针满足
	fmt.Println(s.Speak())

	// s = Cat{} 会编译失败：Cat 的方法集不含 Speak
	var _ Speaker = (*Cat)(nil) // 编译期断言
}
```

<OutputBox title="运行结果" output="wang\nmiao" />

| 场景 | 值接收者 `T` | 指针接收者 `*T` |
| --- | --- | --- |
| 能否改原值 | 否，改的是副本 | 是 |
| `T` 可调用 | 是 | 是，编译器取地址 |
| `*T` 可调用 | 是 | 是 |
| 接口实现 | `T` 与 `*T` 都满足 | 仅 `*T` 满足 |
| 大结构体成本 | 每次拷贝一份 | 只传一个指针 |
| nil 调用 | 不适用 | 可以，需方法内判空 |

<Callout type="warn" title="易错点" text="值接收者改不了原对象；赋接口先确认方法集。" />

`nil` 接收者是另一个必须想到的边界：指针接收者完全可能为 `nil`，方法若不加判断就会解引用崩溃。反过来，允许 `nil` 接收者能写出很优雅的递归代码，比如链表求和。注意这和「接口变量是否为 nil」是两件事，下一讲会专门拆开。

```go
package main

import "fmt"

type Node struct {
	Value int
	Next  *Node
}

// nil 接收者可被直接调用
func (n *Node) Sum() int {
	if n == nil {
		return 0
	}
	return n.Value + n.Next.Sum()
}

func main() {
	list := &Node{1, &Node{2, &Node{3, nil}}}
	fmt.Println("链表和 =", list.Sum())

	var empty *Node
	fmt.Println("nil 接收者 =", empty.Sum())
}
```

<OutputBox title="运行结果" output="链表和 = 6\nnil 接收者 = 0" />

## 4. 工程实践

选接收者有三条实用标准：方法要修改状态、结构体较大、或类型里已有其他指针接收者方法时，用指针；只是读数据、结构体很小、且希望值类型也能满足接口时，用值。同一个类型尽量保持接收者风格一致，混用会让方法集变得难记。接口实现尽量由使用方定义，实现方不知道自己该满足哪些接口。

<Compare title="接收者的选择" :panels='[{"label":"写法 A","code":"func (c Counter) Total() int {\n    return c.N\n}","note":"只读小对象，值也满足接口"},{"label":"写法 B","code":"func (c *Counter) Add(d int) {\n    c.N += d\n}","note":"改状态或对象大，用指针"}]' />

<Callout type="tip" title="切片元素调指针方法" text="切片元素可取地址；map 的 value 不能取址，得先取出或改存指针。" />

## 动手练一练

<Exercise title="让结构体可自增" task="给 Counter 加方法 Inc，调用后 N 加 1 并打印。" :hints='["要改原值用指针接收者","调用自动取地址"]' solution="func (c *Counter) Inc() {\n    c.N++\n}\nc := Counter{}\nc.Inc()\nfmt.Println(c.N)" explain="指针接收者改到原值，输出 1。" />

## 随堂检测

<Quiz no="18-1" q="关于方法集，正确的是？" :options='["T 含指针接收者方法","*T 只能调指针方法","值接收者让 T 与 *T 都满足接口","能给其他包类型加方法"]' answer="2" explain="T 只含值接收者方法；非本地类型不能加方法。" />

<Predict no="18-2" q="Cat 仅由 *Cat 实现 Speaker，把 Cat{} 赋给 Speaker 会怎样？" :options='["正常编译","编译失败","运行时 panic","输出空串"]' answer="1" output="编译失败：Cat does not implement Speaker" explain="值类型方法集只含值接收者方法。" />

## 一页纸速记

<KnowledgeCard :points='["接收者是隐式第一参数","值改副本，指针改原值","*T 含两类方法","指针接收者可为 nil"]' :pitfalls='["值接收者改字段无效","值类型赋给指针实现的接口","给非本地类型加方法"]' summary="改状态或对象大用指针。" />

## 小结与下一讲

- 方法 = 带接收者的函数；值接收者拿副本，指针接收者改原值，调用侧自动转换。
- `T` 的方法集只含值接收者方法，`*T` 含两类；这直接决定接口能否赋值。
- 指针接收者可能为 `nil`，方法内判空；选接收者看修改需求、体积与一致性。

方法集是接口实现的基础，下一讲正式进入 Go 的抽象核心。

[下一讲：接口：隐式实现与组合](/basics/interfaces)
