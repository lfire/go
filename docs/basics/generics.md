---
title: 第 22 讲 · 泛型：类型参数与约束
description: 用类型参数把算法与具体类型解耦，掌握约束、comparable、类型推断与泛型类型，并知道何时不该用泛型。
---

<StageHeader stage="L2 · 语法核心" level="★★★☆☆" time="40 分钟" goal="用泛型消除重复而不过度设计" prereq="第 21 讲 · defer / panic / recover" />

# 第 22 讲 · 泛型：类型参数与约束

> 给 `[]int` 写一个求和函数，再给 `[]float64` 写一个，两段代码除了类型名一模一样。泛型出现之前，只有两条路：复制粘贴，或者改用 `interface{}` 加类型断言——前者维护两份，后者丢掉编译期类型检查、还容易在断言处 panic。泛型给了第三条路：把「类型」也当成参数传进函数，让编译器替你检查每一种具体用法。这一讲学会写它，更重要的是学会判断什么时候别写它。

## 这一讲你会得到什么
<CheckList id="lesson-22" title="本讲目标" :items='["读懂方括号里的类型参数与约束语法","会用 any、comparable 与自定义联合约束","能写泛型函数与泛型类型，并理解类型推断","分清泛型与接口各自适合解决的问题","识别泛型过度设计的信号与工程代价"]' />

## 1. 先建立直觉

把泛型想成一套饼干模具。同一个模具（算法）可以压黄油面团，也可以压巧克力面团（不同类型），但模具对面团是有要求的：得能擀平、不能太稀。这个「要求」就是约束（constraint）。写成代码，模具是函数体，面团是类型参数 `T`，要求写在 `T` 后面的接口里。

泛型最常服务的舞台是切片。回想切片头：它只是 `ptr`、`len`、`cap` 三个机器字，与里面装的是 `int` 还是 `string` 无关。于是「遍历切片、对每个元素做同一件事」这套逻辑天然与元素类型无关，正好交给 `T` 去填。

<ConceptDiagram type="slice-header" title="切片描述符与元素类型无关，正是泛型算法最常作用的结构" />

所以泛型的价值不是「少打几行字」，而是：同一份逻辑只为一份源码负责，编译器却在每一处调用点按具体类型做检查，既没有复制，也没有装箱与断言。

## 2. 语法与规则

类型参数写在函数名后面的方括号里：`[T Number]` 中 `T` 是类型参数，`Number` 是约束。约束本身是一个接口，用类型集来描述「哪些类型可以进来」。`~` 表示底层类型匹配，`|` 表示并集。

```go
package main

import "fmt"

// Number 是自定义约束：底层类型为 int 或 float64 的类型都算满足。
type Number interface {
	~int | ~float64
}

// Sum 对任意满足 Number 的切片求和。
func Sum[T Number](s []T) T {
	var total T // 零值作起点：int 得 0，float64 得 0
	for _, v := range s {
		total += v // 约束保证了加法对这些类型都合法
	}
	return total
}

func main() {
	ints := []int{1, 2, 3}
	floats := []float64{1.5, 2.5}
	fmt.Println(Sum(ints))      // 6：类型推断出 T 为 int
	fmt.Println(Sum(floats))    // 4：T 为 float64
	fmt.Println(Sum[int](ints)) // 6：也可显式写出类型实参
}
```

<OutputBox title="运行结果" output="6\n4\n6" note="同一个函数体被两种类型复用；约束决定了函数体内允许哪些运算。" />

`comparable` 是内建约束，表示类型支持 `==` 与 `!=`，因此可以作为 `map` 的键。下面同时演示多个类型参数、泛型函数和泛型类型。

```go
package main

import "fmt"

// Index 用 comparable 约束，保证相等比较合法。
func Index[T comparable](s []T, target T) int {
	for i, v := range s {
		if v == target {
			return i
		}
	}
	return -1
}

// Map 有 T 与 U 两个类型参数，把 []T 映射成 []U。
func Map[T, U any](s []T, f func(T) U) []U {
	out := make([]U, 0, len(s))
	for _, v := range s {
		out = append(out, f(v))
	}
	return out
}

// Pair 是泛型类型：字段类型由类型参数决定。
type Pair[K comparable, V any] struct {
	Key   K
	Value V
}

func main() {
	fmt.Println(Index([]string{"go", "rust", "zig"}, "zig")) // 2
	words := []string{"a", "bb", "ccc"}
	fmt.Println(Map(words, func(s string) int { return len(s) })) // [1 2 3]
	p := Pair[string, int]{Key: "age", Value: 18}
	fmt.Println(p.Key, p.Value) // age 18
}
```

<OutputBox title="运行结果" output="2\n[1 2 3]\nage 18" />

<LineByLine title="泛型函数声明的五行骨架" code="func Sum[T Number](s []T) T {\n    var total T\n    for _, v := range s {\n        total += v\n    }\n    return total\n}" :notes='["方括号声明类型参数 T，并用 Number 约束它","形参是元素类型 T 的切片，返回值也是 T","用 T 的零值作累加起点，不必知道具体类型","逐个取元素，约束保证加法可用","返回累加结果"]' />

大多数调用不需要写 `Sum[int](ints)`：编译器会从实参 `[]int` 反推出 `T` 是 `int`，这叫类型推断。当类型参数只出现在返回值、或推断线索不足时，才需要显式写出类型实参。Go 1.18 引入泛型，1.21 又补上了 `min`、`max`、`clear` 三个内建函数并放宽了部分推断规则；用较老的编译器时，复杂调用可能仍要手写类型实参。

## 3. 深入一步

理解约束的关键是把接口看成「类型集」：`any` 就是空约束，类型集包含所有类型；`comparable` 是可比较类型的集合；`~int | ~float64` 是底层类型落在这几个之一；带方法的接口则要求类型实现了那些方法。函数体里能做什么，完全由这个集合里「所有类型都支持的操作」决定——这就是为什么把约束写成 `any` 之后，连 `+` 都用不了。

| 约束写法 | 含义 | 允许的关键操作 | 典型用途 |
| --- | --- | --- | --- |
| `any` | 任意类型 | 赋值、传参、作为值存放 | 通用容器与工具函数 |
| `comparable` | 可比较类型 | 相等判断、作 map 的键 | 查找下标、去重、Set |
| 联合类型集（如 int 与 float64） | 底层类型落在集合内 | 加减乘除等算术运算 | 求和、最值 |
| 带方法的接口 | 必须实现指定方法 | 调用约束声明的方法 | 格式化、排序钩子 |

约束也可以是方法集。下面这段同时演示三件事：带方法的约束、泛型类型上如何声明方法，以及直接复用标准库 `slices` 包里的现成泛型函数。

```go
package main

import (
	"fmt"
	"slices"
	"strings"
)

// Stringer 是方法约束：要求类型实现 String()。
type Stringer interface {
	String() string
}

func JoinAll[T Stringer](items []T) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, it.String())
	}
	return strings.Join(parts, ",")
}

// Box 是泛型类型；方法接收者上要重新声明类型参数 T。
type Box[T any] struct {
	Value T
}

func (b Box[T]) Get() T { return b.Value }

// code 的底层类型是 int，且实现了 String()。
type code int

func (c code) String() string { return fmt.Sprintf("CODE-%d", c) }

func main() {
	fmt.Println(JoinAll([]code{7, 8})) // CODE-7,CODE-8
	b := Box[string]{Value: "ok"}
	fmt.Println(b.Get()) // ok

	nums := []int{3, 1, 2}
	slices.Sort(nums)
	fmt.Println(nums)                     // [1 2 3]
	fmt.Println(slices.Contains(nums, 2)) // true
}
```

<OutputBox title="运行结果" output="CODE-7,CODE-8\nok\n[1 2 3]\ntrue" note="方法约束要求实现指定方法；泛型类型的方法要在接收者上重列类型参数；常用算法优先用 slices 包。" />

<Term word="类型集" text="约束接口所描述的类型集合；编译器要求函数体里对 T 的每个操作，都对该集合中的所有类型合法。" />

<Callout type="warn" title="易错点：泛型不是万能抽象，别过度设计" text="只为一种具体类型写一次的辅助函数，直接写具体类型更清楚；约束写成 any 会让函数体无事可做，最后又退回反射与断言；约束把类型列得死死的，又失去了复用价值。行为差异请交给接口，数据容器与算法才交给泛型。另外 Go 1.21 里方法不能声明自己的类型参数，只有函数和类型可以；泛型类型的方法接收者上要重新写出类型参数名。" />

还要认清代价：泛型会带来更长的编译时间、更大的二进制体积和更绕的错误信息，抽象层数每多一层，读代码的人就多背一层心智负担。标准库从 Go 1.21 起提供了 `slices` 与 `maps` 包，像 `slices.Contains`、`slices.Sort`、`slices.Index` 这类通用操作优先直接调用，不要自己再造一套。业务层里出现「泛型框架」「泛型 Repository 基类」这种东西，往往就是抽象过头的信号。

## 4. 工程实践

判断标准可以很朴素：如果删掉类型参数、把 `T` 换成一个具体类型，代码照样能读、而且只有这一个调用方，那就别用泛型；如果同一段逻辑确实要为三种以上类型各写一遍，且操作只依赖「可比较」「可相加」这类稳定性质，泛型才划算。

对比一下用 `any` 硬扛的老写法。它不只是丢掉类型检查：当 `any` 里装的是切片或 map 时，对它做 `==` 会直接 panic，因为动态类型不可比较。泛型的 `comparable` 把这个风险挡在了编译期。

<Compare title="类型安全与装箱的对照" :panels='[{"label":"推荐：泛型加 comparable","code":"func Index[T comparable](s []T, x T) int {\n    for i, v := range s {\n        if v == x {\n            return i\n        }\n    }\n    return -1\n}","note":"编译期保证 T 可比较，无装箱、无断言。"},{"label":"反例：any 加相等比较","code":"func IndexAny(s []any, x any) int {\n    for i, v := range s {\n        if v == x { // 动态类型不可比较会 panic\n            return i\n        }\n    }\n    return -1\n}","note":"入参得先转成 []any，编译期不检查，运行时还可能崩。"}]' />

配合上一讲的取舍一起记：接口表达「你能做什么」（一组行为），泛型表达「你是什么形状」（一组类型）。两者不是替代关系，第 19 讲的「接受接口、返回结构体」依旧适用。

## 动手练一练

<Exercise title="写一个泛型 Contains" task="实现 Contains：对任意可比较类型的切片，判断 target 是否在其中，返回 bool。要求不使用类型断言，也不使用反射。" solution="func Contains[T comparable](s []T, target T) bool {\n    for _, v := range s {\n        if v == target {\n            return true\n        }\n    }\n    return false\n}" :hints='["约束选 comparable，这样相等比较才被允许。","遍历切片逐个比较，命中就提前返回。"]' explain="comparable 保证 == 合法；循环里一旦命中立刻返回 true，走完仍未命中则返回 false。调用时编译器自动推断 T。" />

## 随堂检测

<Quiz no="22-1" q="关于 comparable 与 any，下面哪项说法正确？" :options='["any 约束下的类型参数可以直接做加法","comparable 保证类型支持相等比较，可作 map 的键","comparable 是用户自己定义的接口","any 只允许数值类型"]' answer="1" explain="comparable 是内建约束，描述可比较类型的集合，允许 == 与 != 并可作为 map 键；any 不限制类型，因此也不保证任何特定运算。" />

<Predict no="22-2" q="若把 Sum 的约束从 Number 改成 any，其他代码不动，会怎样？" :options='["照常输出 6 4 6","输出全为 0","编译报错：对 any 做加法不合法","运行时 panic"]' answer="2" output="编译报错：invalid operation: total += v" explain="any 的类型集包含所有类型，函数体里的 += 必须对集合内每个类型都合法，而 string、切片等不支持加法，编译器因此拒绝。约束越宽，函数体里能做的事反而越少。" />

## 一页纸速记

<KnowledgeCard :points='["类型参数写在方括号里，约束是一个描述类型集的接口","any 不限类型，comparable 保证可比较，联合约束用 ~ 与并集","泛型函数与泛型类型都能带类型参数，多数调用可自动推断","标准库 slices 与 maps 已提供常用泛型函数，优先复用"]' :pitfalls='["只有一两个调用方还硬上泛型，抽象成本大于收益","约束写成 any 后在函数体里做运算，直接编译失败","用泛型替代接口去表达行为差异，方向反了","Go 1.21 里方法不能有自己的类型参数"]' summary="泛型抽象的是类型的形状，接口抽象的是行为；先确认重复真实存在，再引入类型参数。" />

## 小结与下一讲

- 类型参数写在函数名或类型名后的方括号里，约束决定函数体能做哪些操作。
- `any`、`comparable`、联合约束与带方法的接口各有各的类型集，类型推断让多数调用不必手写类型实参。
- 泛型有编译、体积与可读性代价；行为差异用接口，通用容器与算法才用泛型，优先复用标准库。

下一讲走进框架背后的能力：程序如何在运行时看见自己的类型与字段，以及怎样用 `go generate` 把重复代码提前生成出来。

[下一讲：反射 reflect 与 go generate](/basics/reflect)
