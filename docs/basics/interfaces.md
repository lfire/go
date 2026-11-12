---
title: 第 19 讲 · 接口：隐式实现与组合
description: 建立 Go 独有的抽象方式：隐式实现、小接口、组合、any 与类型断言
---

<StageHeader stage="L2 · 语法核心" level="★★★☆☆" time="40 分钟" goal="建立 Go 独有的抽象方式" prereq="第 18 讲 · 方法与接收者：值还是指针" />

# 第 19 讲 · 接口：隐式实现与组合

> 有了结构体和方法，代码仍然容易写死：`save` 只认 `FileLogger`，换一种日志实现就得改函数签名。接口把「需要哪些行为」和「谁来实现」分开，让调用方只依赖一组方法签名。Go 的接口尤其特别：不需要 `implements`，类型只要有对应方法就自动满足。学完本讲，你能设计小接口、组合它们、安全地做类型断言，并躲开最经典的 `nil` 陷阱。

## 这一讲你会得到什么
<CheckList id="l19-goals" title="本讲目标" :items='["理解隐式实现与断言","用小接口与组合设计抽象","用 any、断言与 type switch","识别 typed nil 与返回结构体"]' />

## 1. 先建立直觉

把接口想成一份「能力清单」：上面只写了需要哪几个方法，不写谁来实现。任何类型只要方法签名对得上，就自动入列，实现方甚至不知道这份清单的存在。这与需要显式声明继承的语言完全不同：耦合方向反过来了，调用方定义自己需要的能力，实现方只管做自己的事。为什么接口该由使用方定义？因为实现方若提前定一个大接口，调用者就被迫依赖它；Go 让调用方只声明自己会用到的方法，依赖关系因此保持单向、松散。

<ConceptDiagram type="iface" title="接口变量的两个字" caption="一个字描述动态类型，一个字指向数据；类型非空但值为 nil 时接口整体不为 nil" />

<Term word="隐式实现" text="方法集覆盖接口全部方法即自动满足，无需声明。" />

## 2. 语法与规则

下面定义一个只有一个方法的接口。`Console` 没有写任何实现声明，只是恰好有同名同签名的方法，就能传给 `save`。`var _ Writer = Console{}` 是编译期断言，写错方法签名时会在编译阶段立刻报错。

```go
package main

import "fmt"

type Writer interface {
	Write(p []byte) (int, error)
}

type Console struct{}

// 签名一致即自动实现
func (Console) Write(p []byte) (int, error) {
	fmt.Println("写入:", string(p))
	return len(p), nil
}

var _ Writer = Console{}

func save(w Writer, data string) {
	_, _ = w.Write([]byte(data))
}

func main() {
	save(Console{}, "hello")
}
```

<OutputBox title="运行结果" output="写入: hello" />

<LineByLine code="var i any = v\ns, ok := i.(string)\nfmt.Println(s, ok)" :notes='["any 可装任意类型的值","断言尝试取 string，失败也不 panic","ok 为假时 s 是 string 零值"]' title="类型断言逐行拆解" />

接口组合就是把几份小清单拼成一份大清单。标准库是这么做的：`io.ReadWriter` 由 `io.Reader` 与 `io.Writer` 组合而来。小接口的好处是容易实现、容易复用，也更容易替身测试。

```go
package main

import (
	"fmt"
	"strings"
)

type Reader interface {
	Read() string
}

type Closer interface {
	Close()
}

// 接口组合：把小接口拼起来
type ReadCloser interface {
	Reader
	Closer
}

type File struct{ name string }

func (f File) Read() string { return "data from " + f.name }
func (f File) Close()       { fmt.Println("closed", f.name) }

func use(rc ReadCloser) {
	fmt.Println(rc.Read())
	rc.Close()
}

func main() {
	use(File{name: "a.txt"})

	// strings.Reader 满足 io.Reader
	r := strings.NewReader("abc")
	buf := make([]byte, 3)
	n, _ := r.Read(buf)
	fmt.Println("读到", n, "字节:", string(buf[:n]))
}
```

<OutputBox title="运行结果" output="data from a.txt\nclosed a.txt\n读到 3 字节: abc" />

## 3. 深入一步

`any` 是 `interface{}` 的别名，Go 1.18 起提供，两者完全等价。空接口能装任何值，但也意味着你失去了编译期的类型信息，取用时必须靠类型断言或 `type switch` 找回类型。断言有两种写法：单返回值形式断言失败会 panic，只在你确定类型时用；`comma-ok` 形式更安全。

```go
package main

import "fmt"

func describe(x any) {
	// comma-ok 断言：失败也不 panic
	if s, ok := x.(string); ok {
		fmt.Println("字符串长度 =", len(s))
	}

	// type switch：按动态类型分支
	switch v := x.(type) {
	case int:
		fmt.Println("整数 =", v)
	case string:
		fmt.Println("字符串 =", v)
	case nil:
		fmt.Println("nil")
	default:
		fmt.Println("其它类型")
	}
}

func main() {
	describe(42)
	describe("go")
	describe(nil)

	var a any = 7
	n := a.(int) // 确定类型才用单值形式
	fmt.Println("断言得到", n)
}
```

<OutputBox title="运行结果" output="整数 = 42\n字符串长度 = 2\n字符串 = go\nnil\n断言得到 7" />

| 写法 | 形式 | 失败时的行为 |
| --- | --- | --- |
| 单值断言 | `v := i.(T)` | panic |
| comma-ok 断言 | `v, ok := i.(T)` | `ok` 为 false，`v` 为零值 |
| type switch | `switch v := i.(type)` | 走 `default` 或 `nil` 分支 |
| 接口组合 | `interface{ A; B }` | 需同时实现 A 与 B |

<Callout type="warn" title="易错点：typed nil" text="接口含类型与数据两个字；装入 nil 的具体指针后类型字非空，接口整体不为 nil。返回接口别返回带类型的 nil。" />

再看工程里最常见的签名原则：函数接受接口、返回具体结构体。参数用接口，调用方能传任何实现；返回值用具体类型，调用方不会被迫面对一个抽象。下面同时演示 typed nil 的实际表现。

```go
package main

import "fmt"

type Reader interface {
	Read() string
}

type MemReader struct{ data string }

func (m MemReader) Read() string { return m.data }

// 接受接口参数，返回具体结构体
func CopyReader(r Reader) MemReader {
	return MemReader{data: r.Read()}
}

func main() {
	got := CopyReader(MemReader{data: "hello"})
	fmt.Println("返回的具体类型 =", got.Read())

	var p *MemReader // nil 的具体指针
	var i Reader = p
	fmt.Println("i == nil ?", i == nil)

	var empty Reader
	fmt.Println("empty == nil ?", empty == nil)
}
```

<OutputBox title="运行结果" output="返回的具体类型 = hello\ni == nil ? false\nempty == nil ? true" />

<Compare title="接口大小的取舍" :panels='[{"label":"写法 A","code":"type Store interface {\n    Get(k string) string\n    Put(k, v string)\n    Delete(k string)\n    List() []string\n}","note":"方法多，难实现也难换"},{"label":"写法 B","code":"type Getter interface {\n    Get(k string) string\n}","note":"只列用到的能力，好组合"}]' />

## 4. 工程实践

定义接口的最佳位置是「使用方」而不是「实现方」：一个包只声明自己真正会调用的方法，实现方无需引入任何依赖。接口越小越好，单方法接口在标准库里随处可见，命名常以 `-er` 结尾。返回值保持具体类型，避免把 `nil` 藏进接口；确需返回接口时，显式返回 `nil` 字面量。接口底层的 `itab` 与动态派发成本属于 L3 的内容，本讲先掌握使用层的判断。

<Callout type="tip" title="any 不是万能解" text="空接口把类型检查推迟到运行时，滥用会让编译期错误变成线上 panic；只在处理未知类型时用它。" />

## 动手练一练

<Exercise title="定义并实现小接口" task="声明只含 Size() int 的接口 Sizer，让 Box 实现并用断言确认。" :hints='["接口只列一个方法","方法签名要一致"]' solution="type Sizer interface {\n    Size() int\n}\ntype Box struct{ n int }\nfunc (b Box) Size() int { return b.n }\nvar _ Sizer = Box{}" explain="签名一致即自动实现；签名不符时断言会在编译期报错。" />

## 随堂检测

<Quiz no="19-1" q="关于 Go 接口，正确的是？" :options='["必须显式声明实现","接口由实现方定义","小接口更易复用","any 是 Go 1.0 引入的"]' answer="2" explain="隐式实现无需声明；接口由使用方定义；any 是 Go 1.18 引入的空接口别名。" />

<Predict no="19-2" q="把 nil 的 *T 指针赋给 any 变量 i，判断 i == nil 的结果是？" :options='["true","false","编译失败","运行时 panic"]' answer="1" output="false" explain="接口含类型与数据两个字；装入 nil 的 *T 后类型字非空，整体不为 nil。" />

## 一页纸速记

<KnowledgeCard :points='["方法集吻合即隐式实现","接口由使用方定义，越小越好","接口可组合，any 是空接口","接受接口，返回结构体"]' :pitfalls='["返回带类型的 nil","单值断言失败 panic","实现方提前定大接口"]' summary="接口列能力不列身份；小接口与组合是核心，typed nil 是经典坑。" />

## 小结与下一讲

- 类型无需声明实现，方法集吻合即自动满足；编译期断言能提前暴露签名错误。
- 接口由使用方定义、越小越好，可组合；`any` 是 Go 1.18 起的空接口别名。
- 断言优先用 `comma-ok`；`typed nil` 让接口不为 `nil`，返回接口时要显式返回 `nil`。

到这里程序已经能组织数据与行为，接下来补上出错时该怎么办。

[下一讲：错误处理：error、包装与错误链](/basics/errors)
