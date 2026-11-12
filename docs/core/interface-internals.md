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

## 动手练一练

<Exercise title="修掉一个 typed nil" task="下面函数会返回一个「不等于 nil」的 error，请改写它：当内部指针为 nil 时返回真正的 nil，并用 err == nil 验证。" :hints='["问题出在把 nil 指针直接 return 成接口","显式判断后 return nil 即可"]' solution="func load() error {\n    var p *MyErr\n    if p == nil { return nil }\n    return p\n}" explain="显式返回无类型的 nil，接口的两个字才都为空；直接返回 p 会装箱成 typed nil。" />

## 随堂检测

<Quiz no="30-1" q="var p *MyErr = nil; var e error = p，此时 e == nil 的结果是？" :options='["true","false","编译错误","取决于 MyErr 是否实现 Error"]' answer="1" explain="把 p 装箱成接口后，类型字记下 *MyErr、数据字为空，接口整体不等于 nil。这就是 typed nil 陷阱。" />

<Predict no="30-2" q="func bad() error { var p *MyErr; return p }，调用方判断 bad() != nil 结果如何？" :options='["false，一切正常","true，明明没出错却走进错误分支","panic","编译错误"]' answer="1" output="true：err 不等于 nil 成立，但里面其实是个 nil 指针" explain="返回值被装箱成非 nil 接口，调用方误判。正确做法是在函数里显式 return nil。" />

<FillBlank no="30-3" tpl="非空接口的头由 ___ 和数据指针两个字组成，前者保存接口类型、动态类型与方法表；空接口 any 的头则由动态类型字与 ___ 字组成。" :answers='["itab","数据"]' hint="填一个结构名与一个字段名" explain="iface 用 itab 承载方法表；eface 只记录动态类型与数据。两者都是两个字、16 字节。" />

## 一页纸速记

<KnowledgeCard :points='["接口值一律两个字：eface 是类型字 + 数据字，iface 是 itab 字 + 数据字","两个字段都为 nil 时接口才等于 nil；装了 nil 指针的接口不等于 nil","方法调用经 itab 间接派发，编译器在类型可知时会做去虚拟化优化","断言、type switch 与反射都读的是接口头里的动态类型信息"]' :pitfalls='["函数返回 error 时返回了 nil 具体指针，制造 typed nil","只用 != nil 判断具体错误类型，应改用 errors.Is","在热路径高频装箱，带来分配与派发成本"]' summary="判断接口，永远同时看「类型字」和「数据字」，别只看其中一个。" />

## 小结与下一讲

- 接口值是双字结构：`eface` 记类型 + 数据，`iface` 记 `itab` + 数据；两字皆空才是 nil。
- typed nil 源于「类型字非空、数据字空」，写 `error` 返回时显式 `return nil` 是唯一稳妥做法。
- 动态派发经 `itab` 间接跳转，但去虚拟化与泛型会显著削弱开销，别凭直觉下结论。

字符串也是「小头 + 指向别处」，却多了一条铁律：不可变。正因如此，它与 `[]byte` 之间的每次转换都要拷贝；而 `unsafe` 零拷贝能省掉这次拷贝，代价是什么？下一讲收尾核心机制前半。

[下一讲：字符串底层与零拷贝转换](/core/string-internals)
