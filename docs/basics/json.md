---
title: 第 24 讲 · JSON、序列化与常见坑
description: 用 encoding/json 与 tag 写稳定的数据契约，讲清 omitempty、数字精度、时间格式、未知字段兼容与流式编解码。
---

<StageHeader stage="L2 · 语法核心" level="★★☆☆☆" time="35 分钟" goal="写出稳定兼容的 API 数据契约" prereq="第 23 讲 · 反射 reflect 与 go generate" />

# 第 24 讲 · JSON、序列化与常见坑

> 服务之间、前后端之间传数据，几乎都落在 JSON 上。Go 的做法很直接：定义一个结构体，用字段标签说明每个键叫什么、什么时候不写出，编解码交给 `encoding/json`。可这套看似简单的机制里，藏着几个特别爱出事的角落：`omitempty` 会把合法的 0 一起吞掉，大整数经 `float64` 转一手就丢了末位，`time.Time` 的默认格式未必是前端要的，多传一个字段又涉及兼容。这一讲把这些坑连同工程上的约定一次讲清楚。

## 这一讲你会得到什么
<CheckList id="lesson-24" title="本讲目标" :items='["会用 Marshal 与 Unmarshal 完成基本编解码","能写对 json tag，理解字段必须可导出的原因","说清 omitempty 对零值字段的影响与替代做法","避开大整数、时间格式与未知字段的兼容问题","知道何时改用流式 Encoder 与 Decoder"]' />

## 1. 先建立直觉

把编码想成打包行李：结构体字段是一件件物品，`json` 标签是挂在上面的行李牌，写着「对外叫什么名字」「空箱子要不要寄」。`json.Marshal` 负责打包，`json.Unmarshal` 负责拆包并按牌子把物品放回原位。

`omitempty` 相当于「空箱子不寄」，问题在于它认定「空」的标准很宽：数字 0、`false`、空字符串、空切片、`nil` 全都算。于是「重试次数设为 0」这种合法配置，一打包就凭空消失了。

<ConceptDiagram type="iface" title="json.Marshal 接收 any：编码靠运行时读出接口背后的类型与字段标签" />

`Marshal` 与 `Unmarshal` 的签名里都出现 `any`：前者接收任意值，后者要求一个指针。上一讲的 `iface` 图正好解释原因——接口里同时带着类型信息和数据指针，`encoding/json` 用反射把它们拆开、读字段与标签，再逐项写出或填入。由此还能推出一条硬规则：只有可导出（大写开头）的字段才会参与编解码，小写字段反射看不见，会被安静跳过。

## 2. 语法与规则

先看最基础的一次往返。注意 `Token` 被 `json:"-"` 排除、`token` 因小写被跳过、`Role` 为零值又带 `omitempty`，所以最后只剩两个键。

```go
package main

import (
	"encoding/json"
	"fmt"
)

// User 只有可导出字段才会参与编解码。
type User struct {
	Name  string `json:"name"`
	Age   int    `json:"age"`
	Token string `json:"-"` // 完全不参与
	Role  string `json:"role,omitempty"`
	token string // 小写字段：反射不可见，被跳过
}

func main() {
	u := User{Name: "Gopher", Age: 8, Token: "secret", token: "hidden"}
	b, err := json.Marshal(u)
	if err != nil {
		fmt.Println("编码失败:", err)
		return
	}
	fmt.Println(string(b))

	var back User
	if err := json.Unmarshal(b, &back); err != nil {
		fmt.Println("解码失败:", err)
		return
	}
	fmt.Println(back.Name, back.Age)
}
```

<OutputBox title="运行结果" output='{"name":"Gopher","age":8}\nGopher 8' note="解码必须传指针，否则 Unmarshal 改不到原变量，只会返回 InvalidUnmarshalError。" />

接下来是本讲最该记住的一段：`omitempty` 把四个零值字段全部省掉，输出只有一个空对象；而把大整数解进 `map[string]any` 时，数字会先变成 `float64`，末位立刻不准。

```go
package main

import (
	"encoding/json"
	"fmt"
)

type Config struct {
	Retries int     `json:"retries,omitempty"`
	Debug   bool    `json:"debug,omitempty"`
	Ratio   float64 `json:"ratio,omitempty"`
	Name    string  `json:"name,omitempty"`
}

func main() {
	c := Config{Retries: 0, Debug: false, Ratio: 0, Name: ""}
	b, _ := json.Marshal(c)
	fmt.Println(string(b)) // {}：四个零值字段全被省掉

	// 数字解进 any 时一律是 float64，超过 2^53 就丢精度。
	var big int64 = 9007199254740993
	raw := []byte(fmt.Sprintf(`{"id":%d}`, big))
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(m["id"]) // 末位已经变了
}
```

<OutputBox title="运行结果" output="{}\n9.007199254740992e+15" note="9007199254740993 经 float64 一转成了 9007199254740992，浏览器里的 JSON.parse 也会犯同样的错。" />

再看时间、未知字段与流式处理。用 `,string` 让大整数以字符串形式传输，能完整保住精度；`Decoder` 直接挂在 `io.Reader` 上，适合逐个读取连续对象。

```go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Event struct {
	Title string    `json:"title"`
	At    time.Time `json:"at"`
	ID    int64     `json:"id,string"` // 数字以字符串传输，避开 float64
}

func main() {
	payload := `{"title":"发布","at":"2026-01-02T15:04:05Z","id":"9007199254740993","extra":1}`
	var e Event
	dec := json.NewDecoder(strings.NewReader(payload))
	if err := dec.Decode(&e); err != nil {
		fmt.Println("解码失败:", err)
		return
	}
	fmt.Println(e.Title, e.At.UTC().Format(time.RFC3339), e.ID)

	// 流式编码：直接写进 io.Writer，不必先攒出完整字节切片。
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, title := range []string{"A", "B"} {
		_ = enc.Encode(Event{Title: title})
	}
	fmt.Print(buf.String())
}
```

<OutputBox title="运行结果" output='发布 2026-01-02T15:04:05Z 9007199254740993\n{"title":"A","at":"0001-01-01T00:00:00Z","id":"0"}\n{"title":"B","at":"0001-01-01T00:00:00Z","id":"0"}' note="extra 是未知字段，默认被安静丢弃；若调用 dec.DisallowUnknownFields() 就会报错。" />

<LineByLine title="编码的四行骨架" code="b, err := json.Marshal(u)\nif err != nil {\n    return err\n}\nfmt.Println(string(b))" :notes='["把结构体编码成字节切片，同时拿到 error","出错立刻返回，不让坏数据继续往下流","闭合错误分支，下面才是成功路径","string 转换只为打印，真正传输应直接用字节切片"]' />

## 3. 深入一步

把标签选项与代价列成表，写 API 时可以对照着查：

| 标签写法 | 作用 | 容易出问题的地方 |
| --- | --- | --- |
| `json:"name"` | 指定对外键名 | 不加 tag 时用字段名原样输出，大写开头与常见小写风格不符 |
| `json:"-"` | 该字段完全不参与 | 只想隐藏敏感字段时好用，但别误写成别的键名 |
| `json:",omitempty"` | 字段为零值时不输出 | 0、false、空串、空切片、nil 全算零值；对结构体字段不生效 |
| `json:",string"` | 数值以字符串形式传输 | 仅对数值与布尔等有效，类型不匹配会报解码错误 |
| 不加标签 | 按字段名编解码 | 匹配时大小写不敏感，容易与上游约定悄悄错开 |

`omitempty` 的替代做法值得单独记：想区分「没传」和「传了 0」，把字段声明成指针 `*int`，未传是 `nil`（不会出现在结果里），显式传 0 是指向 0 的非 nil 指针（会输出 0）。另一个常见误区是以为 `omitempty` 能省掉空结构体——它不能，结构体零值照样输出，要省得用指针。

数字精度有三条正规出路：定义带 `int64` 字段的具体结构体，让解码器直接按目标类型解析；开启 `dec.UseNumber()`，把数字保留成 `json.Number` 再按需转 `Int64()`；或者像上面那样用 `,string` 以字符串传输。时间方面，`time.Time` 默认按 RFC3339 输出，若契约要求毫秒时间戳或自定义格式，就自定义一个类型实现 `MarshalJSON` 与 `UnmarshalJSON`，或者干脆把字段声明为 `int64` 自己转换，别去改全局行为。

<Term word="json.Number" text="本质是一个字符串类型，开启 Decoder.UseNumber 后数字按原文保留，可再调 Int64 或 Float64，避免先过 float64 丢精度。" />

<Callout type="warn" title="易错点：omitempty 与数值精度" text="omitempty 会吞掉合法的 0 与 false，配置类字段误用它，会让下游把「没这个键」理解成「用默认值」。数字一旦解进 any 或 map[string]any 就变成 float64，订单号、雪花 ID 这类超过 2^53 的整数会静默失真，而且不报任何错。大整数请走具体类型、UseNumber 或 ,string。" />

兼容性上还有一条默认规则要记住：解码时遇到结构体里没有的字段，`encoding/json` 默认安静丢弃。这对向前兼容是好事——服务端加字段，老客户端不会炸；但对入参校验是坏事，拼错的键名也会被吞掉，此时应显式调用 `dec.DisallowUnknownFields()`。相应地，新增字段一般安全，删除字段、改键名、把数值改成字符串都是破坏性变更，需要和调用方约定灰度期。

## 4. 工程实践

真实项目里建议为对外数据单独定义 DTO 结构体，每个字段都写全 tag，不要把内部领域结构体直接 `Marshal` 出去——内部字段改名会立刻变成接口事故。编解码的 `error` 一律检查；`Marshal` 对含 `chan`、`func`、循环引用的值会报错，`Unmarshal` 对格式错误、类型不匹配会报错，两者都不该被 `_` 丢掉。

遇到大响应体或上传的 JSON 流，别用 `Marshal` 攒出整个 `[]byte`，改用 `json.NewEncoder(w)` 直接写 `http.ResponseWriter`；读取侧用 `json.NewDecoder(r.Body)`，配合 `More()` 循环处理 NDJSON。这既省一次内存拷贝，也让 `UseNumber`、`DisallowUnknownFields` 这些开关有地方可挂。

<Compare title="两种组织 JSON 的方式" :panels='[{"label":"推荐：结构体加 tag","code":"type Resp struct {\n    Code int    `json:\"code\"`\n    Msg  string `json:\"msg\"`\n}\nb, err := json.Marshal(Resp{Code: 0, Msg: \"ok\"})","note":"键名由类型固定，字段改名编译期可见，omitempty 行为可控。"},{"label":"反例：map[string]any 一把梭","code":"m := map[string]any{\"code\": 0, \"msg\": \"ok\"}\nb, err := json.Marshal(m)","note":"键名靠手写易拼错，数字默认 float64，字段调整没有编译期提醒。"}]' />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l2-basics/lesson-024-json`](https://github.com/lfire/go/tree/main/examples/l2-basics/lesson-024-json)。clone 仓库后进入 `examples/l2-basics`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

如果还想把上面几条结论追到标准库定义与实现，下面给出可自己复现的推导、两条真实误解的对照，以及带逐字段解释的实验。

### 再推一层：反射、omitempty 与数字精度的实现依据

`encoding/json` 完全建立在上一讲的反射之上：`go doc encoding/json.Marshal` 的签名是 `func Marshal(v any) ([]byte, error)`，它「traverses the value v recursively」，遇到实现了 `json.Marshaler` 的值就调其 `MarshalJSON`；`go doc encoding/json.Unmarshal` 则写「If v is nil or not a pointer, Unmarshal returns an InvalidUnmarshalError」——这就是「解码必须传指针」的规范级依据：不传指针，解码器拿不到可写的目标。字段是否参与编码由反射的导出性决定（`StructField.PkgPath`），键名由 `StructTag.Get("json")` 决定；`omitempty` 的判定在 `encoding/json/encode.go` 的 `isEmptyValue` 函数里，对 `0`、`false`、空串、nil 指针/接口、空 slice/map 一律返回 true，且对结构体类型从不生效——这是「结构体零值照样输出」的原因。数字精度问题来自默认解码路径：解进 `any` / `map[string]any` 时，所有数字统一落成 `float64`（IEEE 754 双精度只有 53 位有效位），超过 2^53 的整数必然失真；`go doc encoding/json.Decoder.UseNumber` 明写「unmarshal a number into an interface{} as a Number instead of as a float64」，这是从实现到 API 的直接证据。`time.Time` 默认按 RFC3339 编码，则是它实现了 `MarshalJSON` 的结果，不是 `encoding/json` 的全局特例。

### 两条真实误解：错误写法与正确写法

**误解一：`json.Unmarshal(data, v)` 里 v 传值也能填进去，只是改的是副本。** 非指针目标在入口就被拒。

<Compare title="解码目标必须是指针" :panels='[{"label":"错误：传值","code":"var u User\njson.Unmarshal(b, u)","note":"返回 InvalidUnmarshalError，u 保持零值"},{"label":"正确：传地址","code":"var u User\njson.Unmarshal(b, &u)","note":"解码器拿到可写目标，字段被填充"}]' />

`Marshal` 接收值是因为它只读，`Unmarshal` 要写入所以必须可寻址；这个错误可以用 `errors.As(err, &unmarshalTypeErr)` 精确定位。

**误解二：字段带 `json:"meta,omitempty"` 时，`omitempty` 能省掉空的嵌套结构体。** 结构体零值不算 empty。

<Compare title="结构体字段与 omitempty" :panels='[{"label":"错误：值类型字段","code":"type Req struct { Meta Meta }\nb, _ := json.Marshal(Req{})","note":"零值结构体照样输出一个键"},{"label":"正确：改成指针字段","code":"type Req struct { Meta *Meta }\nb, _ := json.Marshal(Req{})","note":"指针为 nil 时才不输出"}]' />

`isEmptyValue` 只对 array / map / slice / string、数值、布尔、指针 / 接口判空，结构体不在其列；指针字段正是区分「没传」与「传了零值」的正规做法。

### 可验证实验：命令、输出与逐字段解释

```bash
go doc encoding/json.Unmarshal
go doc encoding/json.Decoder.UseNumber
```

```text
func Unmarshal(data []byte, v any) error
    If v is nil or not a pointer, Unmarshal returns an InvalidUnmarshalError.
func (dec *Decoder) UseNumber()
    UseNumber causes the Decoder to unmarshal a number into an interface{} as a
    Number instead of as a float64.
```

| 输出片段 | 含义 |
| --- | --- |
| `func Unmarshal(data []byte, v any) error` | 输入是字节切片，目标是 any |
| `not a pointer` | 非指针目标被拒 |
| `InvalidUnmarshalError` | 具体错误类型，可用 `errors.As` 取回 |
| `func (dec *Decoder) UseNumber()` | 只作用于 Decoder，须在 Decode 前调用 |
| `as a Number instead of as a float64` | 精度问题的根源与解法都在这一句 |

下面这段把 omitempty 对结构体、非指针目标报错与 UseNumber 保精度跑成确定输出：

```go
package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Meta struct{ Page int }

type Req struct {
	Meta    Meta  `json:"meta,omitempty"`
	MetaPtr *Meta `json:"meta_ptr,omitempty"`
	Retries int   `json:"retries,omitempty"`
}

func main() {
	b, _ := json.Marshal(Req{})
	fmt.Println("omitempty 对结构体仍输出 meta =", strings.Contains(string(b), "meta"))
	fmt.Println("指针字段被省略 =", !strings.Contains(string(b), "meta_ptr"))

	var u struct{ Name string }
	err := json.Unmarshal([]byte(`{"Name":"go"}`), u)
	fmt.Println("传值的错误 =", err != nil)

	dec := json.NewDecoder(strings.NewReader(`{"id":9007199254740993}`))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		fmt.Println("解码失败 =", err)
		return
	}
	n, _ := m["id"].(json.Number).Int64()
	fmt.Println("UseNumber 后 =", n)
}
```

<OutputBox title="运行结果" output="omitempty 对结构体仍输出 meta = true\n指针字段被省略 = true\n传值的错误 = true\nUseNumber 后 = 9007199254740993" />

其中 `json.Marshal(Req{})` 的实际字节是下面这串，值类型字段没有被省掉，嵌套大括号正是 `omitempty` 对结构体不生效的直观证据；把这段放进代码块，也避免正文里出现成对的双花括号被当成模板插值：

```text
{"meta":{"Page":0}}
```

### 版本差异与适用边界

| 版本 | 变化 | 适用边界 |
| --- | --- | --- |
| Go 1.10 | 加入 `Decoder.DisallowUnknownFields` | 严格校验入参 |
| Go 1.13 | 错误链可用于取 `*json.SyntaxError` 的 `Offset` | 精确定位坏 JSON 的位置 |
| Go 1.18 | `any` 成为别名，`map[string]any` 写法流行 | 与 `map[string]interface{}` 等价，精度坑不变 |
| Go 1.21 | 该版本尚无 `omitzero`，区分零值靠指针或 `,string` | 本教程基于 1.21+ |
| 全版本 | 数字解进 any 变 float64、字段须可导出、解码须传指针 | 结论与版本无关 |

适用边界还有一条：`DisallowUnknownFields` 只作用于 `Decoder`，`json.Unmarshal` 没有对应开关；新增字段一般向前兼容，删除字段、改键名、把数值改成字符串都是破坏性变更，需与调用方约定灰度期。

### 与 L3 底层、L10 实战的呼应

[第 30 讲](/core/interface-internals) 解释 `Marshal(v any)` 接收接口时的两个字布局，[第 31 讲](/core/string-internals) 讲 `string` 与 `[]byte` 转换及 UTF-8 边界；[第 57 讲](/engineering/error-design) 把编解码错误收敛成统一响应。实战里，[第 63 讲](/server/rest-api) 把本讲的字段、精度与兼容规则写成 API 契约，[第 112 讲](/projects/kv-2-network) 的协议帧编解码、[第 115 讲](/projects/micro-ecommerce) 的 DTO 设计都建立在「结构体加 tag 就是数据契约」这一条上。

## 动手练一练

<Exercise title="用 Decoder 保住大整数精度" task="补全 DecodePreserve：用 json.Decoder 从 io.Reader 解码到 dst，并要求数字保留为 json.Number 而不是 float64。想想哪个方法要在 Decode 之前调用。" solution="func DecodePreserve(r io.Reader, dst any) error {\n    dec := json.NewDecoder(r)\n    dec.UseNumber()\n    return dec.Decode(dst)\n}" :hints='["入口是 json.NewDecoder，不是 json.Unmarshal。","UseNumber 必须在 Decode 之前调用，顺序反了就没用。"]' explain="UseNumber 让解码器把数字按原文存成 json.Number（底层是字符串），之后可按需 Int64 或 Float64，避免先落进 float64 造成精度丢失。" />

## 随堂检测

<Quiz no="24-1" q="关于 omitempty，下面哪项说法正确？" :options='["只让空字符串字段不输出","结构体零值字段也不会输出","0、false、空切片与 nil 都会被当作零值而不输出","它会影响解码时对缺失字段的处理"]' answer="2" explain="omitempty 只在编码时生效，对零值一视同仁：数字 0、false、空串、空切片、空 map、nil 都不写出。想保留 0 请用指针字段。" />

<Predict no="24-2" q="第二段代码里，json.Marshal(c) 打印出来的第一行是什么？" :options='["{\"retries\":0,\"debug\":false,\"ratio\":0,\"name\":\"\"}","{}","null","编译报错"]' answer="1" output="{}" explain="四个字段恰好都是零值且都带 omitempty，于是全部不出现在结果里，序列化结果就是一个空对象。" />

## 一页纸速记

<KnowledgeCard :points='["字段必须可导出，tag 决定键名；解码必须传指针","Marshal 与 Unmarshal 的 error 一律检查","大整数用具体类型、UseNumber 或 ,string 保住精度","未知字段默认丢弃，需要严格校验就开 DisallowUnknownFields"]' :pitfalls='["omitempty 吞掉合法的 0 与 false，配置字段慎用","数字解进 any 变 float64，超过 2^53 静默失真","time.Time 默认 RFC3339，与前端约定不一致时不报错只是格式错","删字段、改键名是破坏性变更，必须与调用方协商"]' summary="JSON 是契约不是临时字符串：字段、精度与兼容规则都要显式写清楚。" />

## 小结与下一讲

- 结构体加 tag 就是数据契约；只有可导出字段参与，解码要传指针并始终检查 `error`。
- `omitempty` 会让所有零值字段不出现在结果里，想区分 0 与「没传」请用指针；大整数别落进 `float64`。
- 大对象用 `Encoder`/`Decoder` 流式处理，未知字段默认丢弃、可按需开启严格校验。

至此 L2 的语法核心就齐了。下一讲把它们攒成一个能跑的小系统：用接口抽象存储、用错误表达失败、用锁保护并发，做一个内存版 KV，并为后面整条贯穿线留好扩展点。

[下一讲：阶段综合：内存版 KV 存储](/basics/capstone)
