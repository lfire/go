---
title: 第 023 讲 · 反射 reflect 与 go generate
description: 用 reflect.Type 与 reflect.Value 在运行时看见类型、读取标签并修改值，理解可设置性与反射三定律，再用 go generate 把重复代码提前生成。
---

<StageHeader stage="L2 · 语法核心" level="★★★★☆" time="40 分钟" goal="理解框架背后的元编程能力" prereq="第 022 讲 · 泛型：类型参数与约束" />

# 第 023 讲 · 反射 reflect 与 go generate

> 一个 JSON 解码器并不认识你写的 `User` 结构体，一个 ORM 也不知道你有哪些字段，它们却能把一行数据稳稳填进去。靠的是反射：程序在运行时检查自己的类型与值，再按名字、标签去读写。反射是框架的地基，但它同时放弃了编译期类型检查、更慢、错误多半以 panic 形式出现。这一讲把能力、坑和边界一次讲透，并给出另一条更稳的路——`go generate` 在编译前把代码生成好。

## 这一讲你会得到什么
<CheckList id="lesson-23" title="本讲目标" :items='["分清 reflect.Type 与 reflect.Value 各自的职责","记住反射三定律，并说清可设置性的三个条件","能用 CanSet 判断并安全地修改字段","会读取结构体标签，理解框架如何靠标签工作","知道 go generate 的流程与反射之间的取舍"]' />

## 1. 先建立直觉

普通代码是直接对着「本人」操作：你知道它叫 `User`、有 `Name` 字段，于是写 `u.Name`。反射拿到的是两样别的东西：一份「体检报告」描述这个值的类型与字段（reflect.Type），以及一个「可操作的替身」代表此刻的值（reflect.Value）。报告告诉你有什么，替身让你去读写。

关键在于：替身分「只读复印件」和「可写正本」。把值本身传进去，得到的是复印件，改它不会动到本人；只有把地址传进去、再顺着地址找到正本，才允许写。这就是 `CanSet` 要回答的问题，也是这一讲最容易踩的坑。

<ConceptDiagram type="iface" title="反射入口是 any：类型信息与数据指针分开存放" />

`reflect.TypeOf` 与 `reflect.ValueOf` 的形参都是 `any`。接口变量本身是两个字：一个描述动态类型，一个指向数据。反射正是把这两个字拆开、分别包装成 `Type` 与 `Value`，这也是接下来三定律第一条的底层原因。

## 2. 语法与规则

先建立一对基本概念：`Type` 回答「它是什么类型」，`Value` 回答「它现在是什么值」。`Kind` 则是更粗的分类，比如 `struct`、`int`、`ptr`、`slice`，用来写通用分支。

```go
package main

import (
	"fmt"
	"reflect"
)

type User struct {
	Name string `json:"name" validate:"required"`
	Age  int    `json:"age"`
}

func main() {
	u := User{Name: "Gopher", Age: 8}

	t := reflect.TypeOf(u)
	v := reflect.ValueOf(u)
	fmt.Println(t.Name(), t.Kind()) // User struct
	fmt.Println(v.Kind())           // struct
	fmt.Println(v.Field(0).String()) // Gopher
	fmt.Println(v.Field(1).Int())    // 8

	// 从反射对象回到接口值，再做类型断言。
	fmt.Println(v.Interface().(User).Name) // Gopher
}
```

<OutputBox title="运行结果" output="User struct\nstruct\nGopher\n8\nGopher" note="Name 是具体类型名，Kind 是底层分类；对自定义类型要判断 Type，写通用分支才用 Kind。" />

现在看可设置性。同一份数据，传值和传指针得到完全不同的结果；未导出字段即便可寻址也不允许写。

```go
package main

import (
	"fmt"
	"reflect"
)

func main() {
	x := 10
	v := reflect.ValueOf(x)
	fmt.Println(v.CanSet()) // false：传的是值的副本

	p := reflect.ValueOf(&x).Elem() // 顺着指针找到正本
	fmt.Println(p.CanSet())         // true：可寻址且可写
	p.SetInt(42)
	fmt.Println(x) // 42

	// 未导出字段即使可寻址，也不允许写。
	type cfg struct {
		Port int
		name string
	}
	c := cfg{Port: 8080, name: "local"}
	cp := reflect.ValueOf(&c).Elem()
	fmt.Println(cp.Field(0).CanSet()) // true
	fmt.Println(cp.Field(1).CanSet()) // false：小写字段不可导出
}
```

<LineByLine title="反射三定律对应到四行代码" code="v := reflect.ValueOf(x)\nt := v.Type()\np := reflect.ValueOf(&x).Elem()\np.SetInt(42)" :notes='["从接口值得到反射对象：第一定律","反射对象可反查类型信息","想改值必须传指针并 Elem，拿到可寻址的 Value","只有 CanSet 为真时，Set 系列方法才可用"]' />

把标签读出来，就摸到了框架工作的核心：结构体字段上那些反引号里的内容，运行时可以通过 `Type.Field(i).Tag.Get(...)` 取得。下面模拟解码器的一小步，按 `json` 标签找到字段并写入。

```go
package main

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

type User struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

// FillByTag 按 json 标签定位字段并写入，模拟解码器的一小步。
func FillByTag(dst any, key, val string) {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		panic("需要非空指针")
	}
	e := v.Elem()
	t := e.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := strings.Split(field.Tag.Get("json"), ",")[0]
		if tag != key {
			continue
		}
		f := e.Field(i)
		if !f.CanSet() {
			continue
		}
		switch f.Kind() {
		case reflect.String:
			f.SetString(val)
		case reflect.Int:
			n, _ := strconv.Atoi(val)
			f.SetInt(int64(n))
		}
	}
}

func main() {
	var u User
	FillByTag(&u, "name", "Gopher")
	FillByTag(&u, "age", "18")
	fmt.Println(u.Name, u.Age) // Gopher 18
}
```

<OutputBox title="运行结果" output="Gopher 18" note="这里只讲标签如何被读取；完整的编解码规则属于下一讲。" />

## 3. 深入一步

官方把反射归纳成三条定律，值得逐字记住：

1. 反射从接口值到反射对象——`reflect.TypeOf` 与 `reflect.ValueOf` 都接收 `any`。
2. 反射从反射对象回到接口值——`Value.Interface()`，之后通常配一次类型断言。
3. 要修改反射对象，其值必须可设置（settable）。

第 3 条展开成三个同时成立的条件：值是通过指针 `Elem()` 得到的、本身可寻址、字段可导出。任何一条不满足，`CanSet()` 就是 `false`；此时若硬调 `SetInt`、`SetString`，运行时直接 panic。所以框架代码里 `if !f.CanSet() { continue }` 不是多余的防御，而是必须。

| 目标 | 反射入口 | 前提条件 | 常见错误 |
| --- | --- | --- | --- |
| 读字段 | `ValueOf(x).Field(i)` | 值确实是结构体 | 对非结构体调 `Field` 会 panic |
| 改字段 | `ValueOf(&x).Elem().Field(i)` | 指针加 `Elem`，且字段可导出 | 传值导致 `CanSet` 为假；写小写字段 panic |
| 读标签 | `Type.Field(i).Tag.Get(...)` | 拿到的是结构体 `Type` | tag 拼错或取到空串，且不会报错 |
| 调方法 | `Value.Method(i).Call(...)` | 方法必须已导出 | 参数个数或类型不匹配，运行时 panic |
| 造新值 | `reflect.New(t)` | 已有 `Type` | 忘了 `Elem()`，拿到的还是指针 |

<Term word="可设置性" text="reflect.Value 能否被修改的性质；要求值可寻址、由指针 Elem 得到且字段可导出，可用 CanSet 事先检查。" />

<Callout type="warn" title="易错点：可设置性与反射的边界" text="reflect.ValueOf(x) 永远不可设置，改值必须传指针再 Elem；对 CanSet 为假的 Value 调用 Set 会 panic。反射绕开了编译期类型检查，字段改名、类型改了都不会在构建时报错，只会在运行时炸开；性能也比直接访问慢一个量级，且错误信息难读。业务代码里能用具体类型、接口或泛型解决的，不要上反射。" />

`go generate` 提供了另一条思路：与其在运行时猜类型，不如在编译前把专用代码生成出来。指令写在源码注释里，形如 `//go:generate 命令 参数`，执行 `go generate ./...` 时工具会扫描并依次运行。常见工具有为枚举生成 `String()` 的 `stringer`、为接口生成 mock 的 `mockgen`、由 proto 生成代码的 `protoc-gen-go`。生成出来的是普通 `.go` 源码，享受完整的类型检查，运行时零额外开销。

```bash
go install golang.org/x/tools/cmd/stringer@latest
go generate ./...
```

## 4. 工程实践

取舍可以这样定：编译期就能确定类型，用泛型或代码生成；只有「类型在编译期确实未知」时才用反射，典型就是通用解码器、ORM 映射、测试框架与依赖注入容器。这也解释了 `encoding/json` 为什么用反射——它不可能预先知道你的结构体。

工程上还有几条硬规矩：生成的源码要提交进版本库，让不装生成工具的同事也能直接 `go build`；CI 里加一步「重新生成后检查有无 diff」，防止有人手改了生成物；`//go:generate` 指令放在相关类型附近，方便对照；反射相关的错误要在边界处转成普通 `error` 返回，别把 panic 漏给调用方。

```go
package main

import "fmt"

//go:generate stringer -type=Status

// Status 表示任务状态；真实项目里 String() 由 stringer 生成。
type Status int

const (
	Pending Status = iota
	Running
	Done
)

// 这里手写一份，保证本文件可以独立编译运行。
func (s Status) String() string {
	switch s {
	case Pending:
		return "Pending"
	case Running:
		return "Running"
	default:
		return "Done"
	}
}

func main() {
	fmt.Println(Running) // Running
}
```

<Compare title="运行时反射与编译前生成" :panels='[{"label":"运行时反射：灵活","code":"f := reflect.ValueOf(&u).Elem().FieldByName(name)\nif f.CanSet() {\n    f.SetString(val)\n}","note":"任意结构体都能用，但字段名写错只在运行时暴露，且每次调用都有开销。"},{"label":"代码生成：专用","code":"// 生成器为每个字段产出普通赋值\nu.Name = val","note":"编译期检查、速度最快，代价是每种结构体都要生成并维护。"}]' />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l2-basics/lesson-023-reflect`](https://github.com/lfire/go/tree/main/examples/l2-basics/lesson-023-reflect)。clone 仓库后进入 `examples/l2-basics`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

如果还想把上面几条结论追到标准库定义与运行时结构，下面给出可自己复现的推导、两条真实误解的对照，以及带逐字段解释的实验。

### 再推一层：可设置性与标签的规范级来源

反射三定律的底层是接口的「两个字」：`reflect.TypeOf(i)` 与 `reflect.ValueOf(i)` 都接收 `any`，运行时把接口里的类型字包成 `reflect.Type`、数据字包成 `reflect.Value`。`go doc reflect.Value.CanSet` 写得很直白：「A Value can be changed only if it is addressable and was not obtained by the use of unexported struct fields. If CanSet returns false, calling Value.Set or any type-specific setter will panic」——这就是可设置性三个条件的来源：`ValueOf(x)` 拿到的是接口里的数据副本，不可寻址；`ValueOf(&x).Elem()`（`go doc reflect.Value.Elem`：返回指针指向的值）才拿到可寻址的原值；小写字段即使可寻址也被 `PkgPath` 标记挡住。标签则是 `reflect.StructField.Tag`，`go doc reflect.StructTag` 说明它是「key:"value" pairs」的字符串，编译期只存不校验，所以键名拼错不会有编译错误。`go generate` 走的是另一条路：`//go:generate` 指令由 `cmd/go` 扫描执行，产物是普通 `.go` 源码，类型检查与反射无关——这正是「编译期就能确定类型就别用反射」的依据。

### 两条真实误解：错误写法与正确写法

**误解一：`reflect.ValueOf(x)` 拿到的是原值，直接 `SetInt` 就能改 x。** `ValueOf` 的实参是 `any`，值传进去时已被拷贝一份。

<Compare title="传值还是传指针" :panels='[{"label":"错误：对副本调用 Set","code":"v := reflect.ValueOf(x)\nv.SetInt(42)","note":"CanSet 为 false，运行时 panic"},{"label":"正确：指针加 Elem","code":"p := reflect.ValueOf(&x).Elem()\np.SetInt(42)","note":"拿到可寻址正本，改写生效"}]' />

`go doc reflect.Value.CanSet` 明确只有 addressable 且非未导出字段的 Value 才能改；`Elem` 要求的 Kind 是 Pointer 或 Interface，对别的 Kind 调用会 panic。

**误解二：只要传了指针，结构体里所有字段都能用反射写。** 反射同样遵守包可见性。

<Compare title="可导出与未导出字段" :panels='[{"label":"错误：写小写字段","code":"p.FieldByName(\"name\").SetString(\"x\")","note":"未导出字段 CanSet 为 false，Set 会 panic"},{"label":"正确：先判 CanSet","code":"if f := p.FieldByName(\"Name\"); f.CanSet() {\n    f.SetString(\"x\")\n}","note":"字段可导出且先检查，才安全"}]' />

`StructField.PkgPath` 非空即未导出，运行时给它打上只读标记；框架里 `if !f.CanSet() { continue }` 是必须，不是多余的防御。

### 可验证实验：命令、输出与逐字段解释

```bash
go doc reflect.Value.CanSet
go doc reflect.StructTag
```

```text
func (v Value) CanSet() bool
    CanSet reports whether the value of v can be changed. A Value can be
    changed only if it is addressable and was not obtained by the use of
    unexported struct fields. If CanSet returns false, calling Value.Set or any
    type-specific setter (e.g., Value.SetBool, Value.SetInt) will panic.
type StructTag string
    By convention, tag strings are a concatenation of optionally space-separated
    key:"value" pairs.
```

| 输出片段 | 含义 |
| --- | --- |
| `func (v Value) CanSet() bool` | 写操作前的必查项 |
| `addressable` | 必须可寻址，对应 `ValueOf(&x).Elem()` |
| `not obtained by ... unexported struct fields` | 未导出字段一律不可写 |
| `will panic` | 对 CanSet 为假的 Value 调 Set 不是静默失败 |
| `type StructTag string` | 标签本质是字符串，编译器不校验语义 |
| `key:"value" pairs` | 约定格式；`Tag.Get("json")` 只按 key 查 |

下面这段把可设置性的三个条件与标签读取跑成确定输出：

```go
package main

import (
	"fmt"
	"reflect"
)

type Config struct {
	Port int `json:"port"`
	name string
}

func main() {
	c := Config{Port: 8080, name: "local"}

	v := reflect.ValueOf(c)
	fmt.Println("ValueOf(c).CanSet =", v.CanSet())

	p := reflect.ValueOf(&c).Elem()
	fmt.Println("Elem().CanSet =", p.CanSet())
	fPort := p.FieldByName("Port")
	fName := p.FieldByName("name")
	fmt.Println("导出字段 CanSet =", fPort.CanSet(), "未导出字段 CanSet =", fName.CanSet())
	if fPort.CanSet() {
		fPort.SetInt(9090)
	}
	fmt.Println("改后 Port =", c.Port)

	t := reflect.TypeOf(c)
	fmt.Println("json 标签 =", t.Field(0).Tag.Get("json"), "拼错返回空串 =", t.Field(0).Tag.Get("jso") == "")
}
```

<OutputBox title="运行结果" output="ValueOf(c).CanSet = false\nElem().CanSet = true\n导出字段 CanSet = true 未导出字段 CanSet = false\n改后 Port = 9090\njson 标签 = port 拼错返回空串 = true" />

### 版本差异与适用边界

| 版本 | 变化 | 适用边界 |
| --- | --- | --- |
| Go 1.18 | `any` 成为 `interface{}` 别名 | 反射入口 `TypeOf(i any)` 的写法统一 |
| Go 1.20 | `reflect.Value` 增加 `Equal` 与 `Comparable` | 判断动态值能否比较、是否相等 |
| Go 1.22 | `go generate` 行为不变，生成物仍须提交版本库 | 与反射无关，属编译前代码生成 |
| 全版本 | 三定律、CanSet 三条件、标签是字符串 | 结论与版本无关 |

适用边界还有两条：反射绕开编译期类型检查，字段改名、类型改了都不会在构建时报错，只会在运行时炸开；反射错误要在边界处转成普通 `error` 返回，别把 panic 漏给调用方。

### 与 L3 底层、L10 实战的呼应

[第 030 讲](/core/interface-internals) 会展开 `eface` / `iface` 与反射入口的关系，[第 037 讲](/core/unsafe-cgo) 讲 `unsafe` 与反射各自的边界。工程上，[第 053 讲](/engineering/mock-test) 用 gomock 生成替身而非运行时猜类型，[第 060 讲](/engineering/cicd) 把 `go generate` 接进 CI 检查生成物有无 diff。实战里，[第 104 讲](/projects/cli-codegen) 用 AST 加模板把重复代码提前生成出来，[第 117 讲](/projects/cloud-operator) 的 CRD scheme 注册则靠反射把类型信息登记进运行时。

## 动手练一练

<Exercise title="读出所有字段的 json 标签" task="实现 Tags：接收任意结构体值，按字段顺序返回每个字段的 json 标签名，没有标签的字段返回空串。想想该用 TypeOf 还是 ValueOf、该调哪个方法。" solution="func Tags(x any) []string {\n    t := reflect.TypeOf(x)\n    out := make([]string, 0, t.NumField())\n    for i := 0; i != t.NumField(); i++ {\n        out = append(out, t.Field(i).Tag.Get(`json`))\n    }\n    return out\n}" :hints='["标签属于类型信息，所以入口是 reflect.TypeOf。","用 NumField 与 Field 遍历，再调 Tag.Get。"]' explain="只读取不修改，不需要指针与 Elem；Type 上的 StructField 带着 Tag，Field(i).Tag.Get 就能取到反引号里的内容。" />

## 随堂检测

<Quiz no="023-1" q="关于 reflect.Value 的 CanSet，下面哪项说法正确？" :options='["只要字段可导出，CanSet 就为真","reflect.ValueOf(x) 得到的 Value 总是可设置","必须通过指针 Elem 拿到值、且字段可导出，CanSet 才可能为真","CanSet 为假时调用 SetInt 会静默失败"]' answer="2" explain="可设置性要求值可寻址、由指针 Elem 得到且字段可导出；传值得到的是副本，CanSet 为假。对假值调 Set 会 panic，不是静默失败。" />

<Predict no="023-2" q="第二段代码中，reflect.ValueOf(x).CanSet() 与 reflect.ValueOf(&x).Elem().CanSet() 依次输出什么？" :options='["true true","true false","false true","false false"]' answer="2" output="false\ntrue" explain="传值 x 得到的是不可寻址副本；传 &x 再 Elem 得到可寻址的正本，因此只有后者可设置。" />

## 一页纸速记

<KnowledgeCard :points='["Type 描述类型与字段，Value 描述并可操作具体的值","三定律：接口到反射对象、反射对象回接口、修改要求可设置","改值必须传指针并 Elem，且先检查 CanSet","标签通过 Type.Field(i).Tag.Get 读取，是框架映射的依据"]' :pitfalls='["对 reflect.ValueOf(x) 的结果直接 Set，运行时 panic","写未导出字段，CanSet 为假且会 panic","字段名或类型改了不会在编译期报错，反射代码悄悄失效","能用泛型或生成代码解决，却在业务层滥用反射"]' summary="反射把编译期的确定性换成了运行时的灵活，能不用就不用；能生成就别在运行时猜。" />

## 小结与下一讲

- `Type` 管类型信息，`Value` 管值，`Kind` 用于写通用分支；三定律串起「接口 ↔ 反射对象」的往返。
- 可设置性要同时满足指针 `Elem`、可寻址、字段可导出，`CanSet` 是写操作前的必查项。
- 反射留给编译期确实未知类型的框架场景；`go generate` 用生成代码换回类型安全与性能。

下一讲就用上这些基础：看看标准库如何把结构体与 JSON 来回转换，以及 `omitempty`、时间格式和数字精度里那些不起眼却常出事的坑。

[下一讲：JSON、序列化与常见坑](/basics/json)
