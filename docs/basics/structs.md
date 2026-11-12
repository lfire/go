---
title: 第 17 讲 · 结构体、标签与内存对齐
description: 用结构体组织数据，理解字面量、嵌入、json tag、内存对齐与空结构体
---

<StageHeader stage="L2 · 语法核心" level="★★☆☆☆" time="35 分钟" goal="设计合理的数据模型" prereq="第 16 讲 · map：哈希、遍历与 Set 技巧" />

# 第 17 讲 · 结构体、标签与内存对齐

> 前面用 map 存「键到值」，用切片存「一串同类数据」。可一个用户往往同时有 ID、姓名、年龄、注册时间，这些字段类型不同、却属于同一个东西。把它们塞进几个平行切片或 `map[string]any` 很快就会失控。结构体就是给「一组相关字段」起个名字，让编译器替你记住每个字段的类型与位置。学完本讲，你能定义数据模型、读懂标签、并知道字段顺序为什么会改变内存占用。

## 这一讲你会得到什么
<CheckList id="l17-goals" title="本讲目标" :items='["定义并用两种字面量初始化","理解嵌入与字段提升","写对 json tag","看懂对齐与空结构体"]' />

## 1. 先建立直觉

结构体像一张表格的表头：先声明有哪些列、每列是什么类型，之后每一行数据都按这张表头摆放。字段在内存里是连续排布的，但为了读写效率，某些类型必须从特定倍数的地址开始，于是编译器会在字段之间插入填充空隙。列声明的顺序因此会影响整张表占多少字节。

<ConceptDiagram type="struct-align" title="字段顺序与填充空隙" caption="bool 与 int64 混排时，编译器补入 padding 以满足对齐要求" />

<Term word="字段提升" text="嵌入类型的导出字段可被外层直接访问，好像定义在外部一样。" />

## 2. 语法与规则

定义用 `type ... struct`，字段名大写才对包外可见。初始化有两种字面量：键值式按名字赋值、顺序无关；位置式必须按声明顺序把所有字段写全。推荐前者，因为日后加字段不会让所有调用点编译失败。

```go
package main

import "fmt"

type User struct {
	ID   int
	Name string
	Age  int
}

func main() {
	var u User // 零值：每个字段各取零值
	fmt.Printf("零值 %+v\n", u)

	u1 := User{ID: 1, Name: "alice", Age: 18} // 键值式
	u2 := User{2, "bob", 20}                  // 位置式，须写全
	fmt.Println(u1.Name, u2.Name)

	u3 := &User{Name: "carol"}
	u3.Age = 21 // 指针自动解引用
	fmt.Printf("%+v\n", u3)
}
```

<OutputBox title="运行结果" output="零值 {ID:0 Name: Age:0}\nalice bob\n&{ID:0 Name:carol Age:21}" />

<CodeTabs title="两种字面量" :tabs='[{"label":"键值式","code":"u := User{ID: 1, Name: n}","note":"顺序无关，加字段安全"},{"label":"位置式","code":"u := User{1, n, 18}","note":"必须按序写全"}]' />

<LineByLine code="var zero User\nu := User{ID: 1, Name: name}\nfmt.Println(u.Name)" :notes='["零值变量每个字段取零值","按键名赋值，顺序无关","点号访问导出字段"]' title="键值字面量逐行拆解" />

嵌入是 Go 表达「组合」的方式：把另一个类型只写类型名、不写字段名，它的字段和方法就被提升到外层。注意这只是语法糖，不是继承；初始化时仍要显式写出被嵌入的类型名。

```go
package main

import "fmt"

type Point struct {
	X, Y int
}

type Circle struct {
	Point  // 嵌入：字段名就是类型名
	Radius int
}

func main() {
	c := Circle{Point: Point{X: 1, Y: 2}, Radius: 5}
	fmt.Println("X =", c.X, "等价", c.Point.X) // 提升字段
	c.X = 9
	fmt.Printf("%+v\n", c)

	// 命名字面量必须逐层写，不能用提升字段直接初始化
	c2 := Circle{Point: Point{3, 4}, Radius: 1}
	fmt.Println(c2.Y)
}
```

<OutputBox title="运行结果" output="X = 1 等价 1\n{Point:{X:9 Y:2} Radius:5}\n4" />

## 3. 深入一步

字段后面可以挂标签，最常见的用途是告诉 `encoding/json` 该字段在 JSON 里叫什么、什么时候跳过。标签是编译期附在字段上的元信息，运行时通过反射读取；名字写错不会报错，只会悄悄得不到预期结果。序列化的完整规则留到 JSON 一讲，这里只看标签如何改名。

```go
package main

import (
	"encoding/json"
	"fmt"
)

type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Token string `json:"-"`
}

func main() {
	u := User{ID: 7, Name: "alice", Token: "secret"}
	b, err := json.Marshal(u)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(b))

	var back User
	if err := json.Unmarshal(b, &back); err != nil {
		panic(err)
	}
	fmt.Printf("%+v\n", back)
}
```

运行结果（JSON 字符串用文本块展示，避免与组件属性冲突）：

```text
{"id":7,"name":"alice"}
{ID:7 Name:alice Token:}
```

可以看到字段名变成了标签里写的样子，`Token` 被 `-` 跳过；反序列化时未出现的字段保持零值。

<Callout type="warn" title="易错点" text="结构体是值，传参整份拷贝；含切片或 map 字段不可比较；加字段会让位置字面量失效；标签写错不报错。" />

再看内存对齐。CPU 读取未按自然边界对齐的数据会更慢甚至出错，编译器因此给每个字段按其对齐要求补空隙。把大字段排前面、小字段聚在一起，通常能明显缩小体积。

```go
package main

import (
	"fmt"
	"unsafe"
)

type A struct {
	Flag  bool
	Count int64
	Tail  bool
}

type B struct {
	Count int64
	Flag  bool
	Tail  bool
}

func main() {
	fmt.Println("A size =", unsafe.Sizeof(A{}), "align =", unsafe.Alignof(A{}))
	fmt.Println("B size =", unsafe.Sizeof(B{}), "align =", unsafe.Alignof(B{}))
	fmt.Println("空结构体 size =", unsafe.Sizeof(struct{}{}))

	s1, s2 := struct{}{}, struct{}{}
	fmt.Println("零大小值相等 =", s1 == s2)
}
```

<OutputBox title="64 位平台运行结果" output="A size = 24 align = 8\nB size = 16 align = 8\n空结构体 size = 0\n零大小值相等 = true" />

| 布局 | 字段顺序 | `unsafe.Sizeof` | 原因 |
| --- | --- | --- | --- |
| A | bool / int64 / bool | 24 | 前后各补 7 字节 padding |
| B | int64 / bool / bool | 16 | 小字段相邻，尾部补 6 字节 |
| 空结构体 | struct{} | 0 | 不占存储，适合作信号与 Set 值 |

<Compare title="字段顺序的取舍" :panels='[{"label":"写法 A","code":"type A struct {\n    Flag  bool\n    Count int64\n    Tail  bool\n}","note":"大字段夹中间，补两段空隙"},{"label":"写法 B","code":"type B struct {\n    Count int64\n    Flag  bool\n    Tail  bool\n}","note":"大字段在前，体积降到 16"}]' />

## 4. 工程实践

设计数据模型时，先按可读性组织字段，再对热点结构考虑对齐：高频创建的大对象，字段顺序带来的内存节省会被数量放大。但不要为了省几个字节牺牲可读性，只有用 `unsafe.Sizeof` 或压测确认瓶颈后才动手。另外，返回大结构体时考虑用指针避免拷贝；含未导出字段的结构体跨包构造要提供构造函数。

<Callout type="tip" title="空结构体的用途" text="struct{} 不占内存，常作无数据的通道信号或 Set 的值类型；零大小值可比较且相等。" />

把对齐讲透一点，就能自己算 Sizeof，而不是死记 16 和 24。规则只有两条：每个字段必须放在「自身对齐数」的整数倍地址上，不给就插 padding；整个结构体的大小再向上补齐到「最大字段对齐数」的整数倍。`int64` 的对齐数是 8，`bool` 是 1，所以 `bool/int64/bool` 得在第一个 bool 后补 7 字节、末尾再补 7 字节，合 24；换成 `int64/bool/bool` 只在尾部补 6 字节，得 16。想逐个字段核对，用 `unsafe.Offsetof(u.Count)`、`unsafe.Alignof(u.Count)`、`unsafe.Sizeof(u)` 三个函数打印一遍，每个字段的起点一目了然——这比背结论可靠。还要注意结果和平台绑定：同一段代码用 `GOARCH=386 go run` 跑，int64 的对齐数变成 4，A 与 B 的差距会缩小；谈 Sizeof 时先说清是多少位平台。

标签这一层也有可验证的依据。`reflect.StructField.Tag` 就是反引号里那串元信息，`reflect.TypeOf(User{}).Field(0).Tag.Get("json")` 能把它读回来；编译期只把它当字符串存着，完全不校验内容，所以 `json:"prot"` 拼错不会有任何编译错误，只会在序列化时悄悄把键写成错的那个。`cmd/compile/internal/types` 负责计算字段偏移与对齐，`encoding/json` 在运行时通过反射读标签——本讲不展开序列化的完整规则，那是 JSON 一讲的事。

真实踩坑里最贵的一类是「小字段夹在大字段中间」：一个高频创建的结构体平白多出 8 字节，量到百万级就是近 8MB 常驻，用 `go build -gcflags=-m` 还能看到这类大对象更容易被判定为逃逸到堆。修法不是到处重排，而是先用 `unsafe.Sizeof` 或压测确认它真是热点，再动手，别为省几个字节把字段按大小硬排得没法读。另一类安静的坑是嵌入指针没初始化：把 `*Logger` 嵌进 `Service` 却忘了赋值，直接访问提升上来的 `s.Prefix` 就是 nil 解引用 panic，先判空兜住才稳。

<FlowTimeline title="结构体字段排布的四步" :steps='[{"title":"定顺序","desc":"按声明先后排","tone":"blue"},{"title":"查对齐","desc":"每类有自己的对齐数","tone":"orange"},{"title":"补空隙","desc":"不满足就插 padding","tone":"green"},{"title":"尾部对齐","desc":"整体补到最大对齐倍数","tone":"purple"}]' direction="horizontal" />

下面两段代码分别把「错误分支」和「嵌入指针的 nil 边界」写成能跑的例子。第一段给带标签的配置结构加一个 load 函数：非法 JSON 与 nil 目标指针都要在 json.Unmarshal 之前挡住并返回错误。

```go
package main

import (
	"encoding/json"
	"fmt"
)

type Config struct {
	Port int    `json:"port"`
	Name string `json:"name"`
}

func load(data []byte, dst *Config) error {
	// 真边界：调用方可能传 nil 指针，或给一段非法 JSON
	if dst == nil {
		return fmt.Errorf("目标指针不能为 nil")
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("解析配置失败: %v", err)
	}
	return nil
}

func main() {
	var c Config
	if err := load([]byte(`{"port":8080,"name":"svc"}`), &c); err == nil {
		fmt.Println("配置 =", c.Port, c.Name)
	}

	if err := load([]byte(`{"port":`), &c); err != nil {
		fmt.Println("非法 JSON 被挡住 =", err != nil)
	}

	if err := load([]byte(`{}`), nil); err != nil {
		fmt.Println("nil 目标被挡住 =", err != nil)
	}
}
```

<OutputBox title="运行结果" output="配置 = 8080 svc\n非法 JSON 被挡住 = true\nnil 目标被挡住 = true" />

第二段演示嵌入指针：字段提升照样生效，但嵌入的 `*Logger` 没初始化时访问提升字段会崩，先在函数里判空就能优雅兜底。

```go
package main

import "fmt"

type Logger struct {
	Prefix string
}

type Service struct {
	*Logger // 嵌入指针：字段提升照样生效，但可能是 nil
	Name    string
}

func logMsg(s Service, msg string) {
	if s.Logger == nil { // 真边界：忘了初始化嵌入指针
		fmt.Println("[默认]", s.Name, msg)
		return
	}
	fmt.Println("["+s.Prefix+"]", s.Name, msg)
}

func main() {
	s1 := Service{Logger: &Logger{Prefix: "INFO"}, Name: "api"}
	logMsg(s1, "started")

	s2 := Service{Name: "job"} // 忘了初始化嵌入指针
	logMsg(s2, "started")      // 走判空分支，不 panic
}
```

<OutputBox title="运行结果" output="[INFO] api started\n[默认] job started" />

| 标签写法 | 含义 | 易错点 |
| --- | --- | --- |
| `json:"port"` | 键改名为 port | 拼错（如 prot）不报错，键名就按错的来 |
| `json:"-"` | 该字段跳过编码 | 只是不编码，字段本身仍在结构体里 |
| 不写标签 | 用字段名原样 | 未导出字段编码时不可见 |
| 反引号包裹 | 编译期附在字段上的元信息 | 内容靠运行期反射读取，编译期不校验 |

<Predict no="17-3" q="调用 load(一段非法 JSON, &c) 时 err 不为 nil；改成 load(合法 JSON, nil) 把目标指针传成 nil，会怎样？" :options='["正常解析，c 被填充","panic: nil pointer","返回错误，不会 panic","编译失败"]' answer="2" output="返回错误，不会 panic" explain="load 开头先判 dst == nil 直接返回错误，把边界挡在 json.Unmarshal 之前；若不判空，nil 目标交给 Unmarshal 会返回 InvalidUnmarshalError，同样不该让调用方以为解析成功。" />

## 动手练一练

<Exercise title="重排字段省内存" task="把含 bool、int64、bool 的结构体重排，让 64 位平台 Sizeof 从 24 降到 16 并打印验证。" :hints='["int64 需 8 字节对齐","让两个 bool 相邻"]' solution="type T struct {\n    Count int64\n    Flag  bool\n    Tail  bool\n}\nfmt.Println(unsafe.Sizeof(T{}))" explain="int64 放首位，两个 bool 相邻，输出 16。" />

## 随堂检测

<Quiz no="17-1" q="关于结构体，正确的是？" :options='["嵌入就是继承","位置字面量可只写部分字段","未导出字段对包外不可见","含切片字段的结构体可比较"]' answer="2" explain="嵌入是组合；位置字面量须写全；含不可比较字段时不可比较。" />

<Predict no="17-2" q="64 位平台上，struct{ bool; int64; bool } 的 unsafe.Sizeof 结果是？" :options='["16","24","10","17"]' answer="1" output="24" explain="int64 需 8 字节对齐，前后各补 7 字节，合计 24。" />

## 一页纸速记

<KnowledgeCard :points='["结构体把字段打包成一个类型","键值字面量顺序无关","嵌入是组合不是继承","字段顺序影响对齐与体积"]' :pitfalls='["赋值是整份拷贝","含切片字段不可比较","标签写错不报错"]' summary="结构体组织数据；字段顺序关乎可读性与内存。" />

## 小结与下一讲

- 结构体是值，键值字面量更安全；嵌入提供字段提升，本质是组合。
- `json` 标签改变字段名，`-` 可跳过；标签写错不会报错，要主动核对。
- 对齐会插入 padding，`struct{}` 零大小；字段顺序能显著影响体积。

数据模型有了，接下来让这些模型拥有行为。

[下一讲：方法与接收者：值还是指针](/basics/methods)
