---
title: 第 020 讲 · 错误处理：error、包装与错误链
description: 把失败当作普通返回值，用 fmt.Errorf 的 %w 逐层包装保留根因，再用 errors.Is 与 errors.As 做可判定的分支。
---

<StageHeader stage="L2 · 语法核心" level="★★★☆☆" time="40 分钟" goal="写出可诊断、可判定的错误处理" prereq="第 019 讲 · 接口：隐式实现与组合" />

# 第 020 讲 · 错误处理：error、包装与错误链

> 程序只要碰到外部世界就会失败：文件被删、网络断线、用户输入不合法。很多语言用「异常」把失败从正常流程里扔出去，调用链上任何一层都可能悄悄把它漏掉，读代码的人也看不出哪个函数会失败。Go 选了另一条路：失败只是一个普通的返回值，明明白白写在函数签名里。学完这一讲，你能让错误既带上足够的上下文，又不丢掉最原始的病因，还能在任意一层准确判断「这是不是某一种特定错误」。

## 这一讲你会得到什么
<CheckList id="lesson-20" title="本讲目标" :items='["说清 error 为什么是值、为什么必须判空","用 fmt.Errorf 的 %w 逐层包装并保留错误链","用 errors.Is 与 errors.As 写出可判定的错误分支","能定义带字段的自定义错误类型与包级哨兵错误","知道错误码与业务状态码的分工边界"]' />

## 1. 先建立直觉

把一个错误想象成快递包裹上那张签收单。包裹从仓库出发，经过分拣站、干线车、配送点，最后到你手上。每一站发现问题时，不该撕掉原单重写一张，而应在原单上贴一条备注：「我在分拣时打不开这层包装」。备注可以贴很多条，但最底下那行原始原因始终还在；收件人顺着备注往回翻，就能查到最初出错的地方。

Go 的 `fmt.Errorf` 配合 `%w` 就是「贴备注但保留原单」，`errors.Is` 就是「顺着单子查有没有某一行」。相对的，用 `%v` 把错误拼进字符串，等于把原单撕了只留一句话，上层就再也查不到根因。

<ConceptDiagram type="error-chain" title="错误链：底层错误经 %w 包装，由 errors.Is 逐层回溯" />

有了这个模型，再看定义就不奇怪了：`error` 不是异常对象，不带栈回溯、没有抛出点，它只是一个实现 `Error() string` 方法的接口值。正因为足够轻量，Go 才敢要求你把每一个错误都显式处理一遍。约定也很朴素：成功时第二个返回值是 `nil`，失败时非 `nil`；两者都能被静态检查工具盯着，谁也藏不住。

## 2. 语法与规则

标准库给了两个最常用的构造器：`errors.New` 造一个固定错误值，`fmt.Errorf` 造带格式化信息的错误。先看错误「是值」这件事。

```go
package main

import (
	"errors"
	"fmt"
)

// ErrEmptyName 是包级「哨兵错误」：预先创建好、供全包复用的错误值。
var ErrEmptyName = errors.New("name 不能为空")

// greet 把失败作为第二个返回值交还给调用方，而不是中断程序。
func greet(name string) (string, error) {
	if name == "" {
		return "", ErrEmptyName
	}
	return "你好，" + name, nil
}

func main() {
	s, err := greet("Gopher")
	fmt.Println(s)                   // 你好，Gopher
	fmt.Println(err == nil)          // true：没有失败时 error 为 nil

	_, err = greet("")
	fmt.Println(err != nil)          // true：非 nil 即代表失败
	fmt.Println(err == ErrEmptyName) // true：同一个哨兵值可以直接比较
}
```

注意 `err == nil` 与 `err != nil`：这里比较的是接口值本身，不是它里面的字符串。只要函数返回了非 `nil`，就说明这一步失败，必须处理；编译器不会替你检查，但 `go vet` 与静态检查工具会盯着被丢掉的返回值。

真正让错误变得「可诊断」的是包装。下面每一层都用 `%w` 补上「我当时在做什么」，同时把底层错误原封不动挂在链上。

```go
package main

import (
	"errors"
	"fmt"
	"os"
)

// openConfig 用 %w 包装：既补上下文，又不切断根因。
func openConfig(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开配置 %s: %w", path, err)
	}
	return f.Close()
}

func start() error {
	if err := openConfig("missing.yaml"); err != nil {
		return fmt.Errorf("启动服务: %w", err)
	}
	return nil
}

func main() {
	err := start()
	fmt.Println(err != nil)                     // true
	fmt.Println(errors.Is(err, os.ErrNotExist)) // true：穿透两层包装找到根因

	// errors.As 找出链上第一个 *os.PathError，取出结构化字段。
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		fmt.Println(pathErr.Path) // missing.yaml
	}
}
```

<OutputBox title="运行结果" output="true\ntrue\nmissing.yaml" note="errors.Is 沿 Unwrap 链逐层比较；errors.As 找出链上第一个可赋给目标类型的节点，并把值写进你给的指针。" />

<LineByLine title="错误返回的最小骨架" code="v, err := do()\nif err != nil {\n    return v, err\n}\nreturn v, nil" :notes='["调用可能失败的函数，同时拿到结果与 error","判空：非 nil 说明这一步失败了","把错误原样交给上层，不吞掉也不重复打印","成功路径返回数据，并显式返回 nil"]' />

`errors.Is` 的逻辑值得记住：它拿目标错误与链上每个节点比较，若节点自己实现了 `Is(error) bool` 就交给它判断，否则沿 `Unwrap()` 继续往下走，直到匹配或链结束。`errors.As` 同理，只是比较的是「能否赋给某个类型」，并把匹配到的节点写进目标指针。所以包装时务必用 `%w`，判定务必用这两个函数，二者是配套的。

## 3. 深入一步

当错误需要携带结构化信息时，就自己定义类型。只要实现 `Error() string`，它就是 `error`；再实现 `Unwrap() error`，就能把根因接回链里。

```go
package main

import (
	"errors"
	"fmt"
)

// ErrInvalid 是代表「输入非法」这一类别的根因哨兵。
var ErrInvalid = errors.New("非法输入")

// ValidationError 携带字段名、说明与根因的自定义错误类型。
type ValidationError struct {
	Field string
	Msg   string
	Cause error
}

// 实现 error 接口：唯一要求就是 Error() string。
func (e *ValidationError) Error() string {
	return fmt.Sprintf("字段 %s 校验失败: %s", e.Field, e.Msg)
}

// 实现 Unwrap，让 errors.Is / errors.As 能继续往链下走。
func (e *ValidationError) Unwrap() error { return e.Cause }

func check(age int) error {
	if age < 0 {
		return &ValidationError{Field: "age", Msg: "不能为负", Cause: ErrInvalid}
	}
	return nil
}

func main() {
	err := check(-1)
	fmt.Println(err != nil) // true

	var ve *ValidationError
	fmt.Println(errors.As(err, &ve))        // true：取回自定义类型
	fmt.Println(ve.Field)                   // age
	fmt.Println(errors.Is(err, ErrInvalid)) // true：穿透自定义类型找到根因
}
```

把本节手段放在一起对比：

| 手段 | 是否保留原错误 | 上层如何判定 | 适用场景 |
| --- | --- | --- | --- |
| `fmt.Errorf` 配 `%v` | 否，链被切断 | 只能比字符串 | 仅给人看的日志，之后不需要分支 |
| `fmt.Errorf` 配 `%w` | 是，形成一条链 | `errors.Is` 或 `errors.As` | 库函数向上返回，最常用 |
| `errors.New` 哨兵变量 | 自身即根因 | `errors.Is` 或直接比较 | 全包复用的固定错误，如 `io.EOF` |
| 自定义类型加 `Unwrap` | 是，且带字段 | `errors.As` 取字段 | 需要字段名、重试次数等结构化信息 |
| `errors.Join` 合并多个错误 | 是，多条并列 | `errors.Is` 命中任一条 | Go 1.20 起批量校验、并发汇总 |

<Term word="哨兵错误" text="用 errors.New 预先创建、包级导出、供调用方用 errors.Is 判定的固定错误值，例如 io.EOF 与 sql.ErrNoRows。" />

<Callout type="warn" title="易错点：%w 与 errors.Is 必须成对使用" text="中间任何一层写成 %v、或用 errors.New 重新造一个错误，根因就断了，errors.Is 立刻失效。判定错误一律用 errors.Is 或 errors.As，不要用等值比较去比包装后的错误，更不要用 strings.Contains 去匹配错误文本；文本是给人看的，不是给程序做分支的。" />

最后划清边界：这一讲解决的是「程序内部能不能识别错误类别」，靠的是错误链与类型，不是数字编号。业务错误码、HTTP 状态、gRPC 状态属于对外契约，要有稳定的编号与文案映射，那是 [第 057 讲](/engineering/error-design) 的内容。本阶段守住三条就够：库函数用哨兵或自定义类型表达类别，中间层用 `%w` 加上下文，最外层统一打印一次。把业务码提前塞进错误链，会让内部逻辑被外部协议绑住，是过早耦合。

## 4. 工程实践

设想一条真实的调用链：HTTP handler 调 service，service 调 repository，repository 调 `database/sql`。常见毛病有两个：一是每一层都 `log.Println(err)`，同一个故障在日志里刷出四五条，排查时反而找不到重点；二是某层「体贴地」把错误转成一句人话字符串返回，上层的重试逻辑再也判断不出这是不是「连接超时」。

稳妥的做法是：库函数只返回错误、不打印；需要加上下文的地方统一用 `%w`；只有最外层（handler 或 `main`）打印一次，并把 `err` 完整带上。错误文案按 Go 惯例写成小写、不带结尾句号、不带换行，因为它经常被 `%w` 拼进更长的一句话里。另外，被 `_` 丢掉的返回值是代码审查里最该拦下的一类问题，哪怕暂时不处理，也要显式写清楚意图。

<Compare title="包装与打日志的两种写法" :panels='[{"label":"推荐：每层 %w，边界打一次","code":"func repo() error {\n    return fmt.Errorf(\"查询用户: %w\", sql.ErrNoRows)\n}\nfunc handler() {\n    if err := service(); err != nil {\n        log.Printf(\"请求失败: %v\", err)\n    }\n}","note":"链完整，上层仍能用 errors.Is 判定类别；日志只出现一条。"},{"label":"反例：%v 切断且层层打印","code":"return fmt.Errorf(\"查询用户: %v\", err)\n// 每一层都 log.Println(err)","note":"根因丢失，日志重复刷屏，上层无法按类别重试或降级。"}]' />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l2-basics/lesson-020-errors`](https://github.com/lfire/go/tree/main/examples/l2-basics/lesson-020-errors)。clone 仓库后进入 `examples/l2-basics`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

如果还想把上面几条结论追到标准库定义与规范，下面给出可自己复现的推导、两条真实误解的对照，以及带逐字段解释的实验。

### 再推一层：%w 为什么能保住根因、errors.Is 怎么走链

`error` 是值而不是异常，依据就在它的定义：`type error interface { Error() string }`——预声明接口，没有栈、没有抛出点，任何实现 `Error() string` 的类型都能当错误。Go 1.13 把错误链写进标准库：`fmt.Errorf` 遇到 `%w` 时，返回的不是普通的 `*errors.errorString`，而是 `*fmt.wrapError`，它持有 `msg string` 与 `err error` 并实现 `Unwrap() error`；`errors.Is(err, target)` 从 `err` 出发，反复调用 `Unwrap()` 走完整棵树，逐节点用 `==` 或节点自定义的 `Is(error) bool` 比较。`go doc fmt.Errorf` 明写「%w verb with an error operand → the returned error will implement an Unwrap method」，`go doc errors.Is` 明写「repeatedly calling its Unwrap() error」，这就是「`%w` 与 `errors.Is` 必须成对」的规范级依据，而不是经验。`errors.As` 同理，只是把「能否赋给目标类型」作为匹配条件，命中后把节点写进调用方给的指针。相对地 `%v` 只把 `err.Error()` 当字符串拼进格式化结果，返回的错误没有 `Unwrap`，链到此为止——`errors.Is` 退化为只比最外层一个节点。Go 1.20 起又多了 `Unwrap() []error` 这条多错误协议，`errors.Join` 生成的树由 `errors.Is` 深度优先遍历，`errors.Unwrap` 反而不认它。

### 两条真实误解：错误写法与正确写法

**误解一：包装错误后还能用 `==` 或匹配 `err.Error()` 文本来判定类别。** `%w` 生成的是新的 wrapError 值，与哨兵不是同一个对象。

<Compare title="判定包装后的错误" :panels='[{"label":"错误：等值或文本匹配","code":"if err == ErrNotFound { ... }\nif strings.Contains(err.Error(), \"not found\") { ... }","note":"包装后等值不成立；文案一改分支就碎"},{"label":"正确：errors.Is 沿链回溯","code":"if errors.Is(err, ErrNotFound) { ... }","note":"不管包了几层都能命中根因"}]' />

文本是给人看的，不是给程序做分支的；一旦有人把「not found」改成「未找到」，字符串匹配的代码就悄悄失效了。

**误解二：`errors.Join` 合并的错误，`errors.Unwrap` 能取回其中某一条。** Join 走的是多错误协议，单错误版的 Unwrap 不认它。

<Compare title="Join 与 Unwrap 的关系" :panels='[{"label":"错误：用 Unwrap 取子错误","code":"joined := errors.Join(a, b)\nfmt.Println(errors.Unwrap(joined))","note":"返回 nil，Join 实现的是 Unwrap() []error"},{"label":"正确：用 errors.Is 遍历子树","code":"if errors.Is(joined, a) { ... }","note":"errors.Is 会遍历 Join 的每个孩子"}]' />

`go doc errors.Unwrap` 明确写着「Unwrap does not unwrap errors returned by Join」；多错误的树只能交给 `errors.Is` / `errors.As`。

### 可验证实验：命令、输出与逐字段解释

```bash
go doc fmt.Errorf
go doc errors.Unwrap
```

```text
func Errorf(format string, a ...any) error
    If the format specifier includes a %w verb with an error operand,
    the returned error will implement an Unwrap method returning the operand.
func Unwrap(err error) error
    Unwrap only calls a method of the form "Unwrap() error". In particular
    Unwrap does not unwrap errors returned by Join.
```

| 输出片段 | 含义 |
| --- | --- |
| `func Errorf(format string, a ...any) error` | 格式化构造函数，`any` 即任意实参 |
| `%w verb with an error operand` | 只有 `%w` 且操作数是 error 才生成包装链 |
| `implement an Unwrap method returning the operand` | 返回的错误持有原错误，errors.Is 才能回溯 |
| `Unwrap only calls ... "Unwrap() error"` | 只处理单错误协议 |
| `does not unwrap errors returned by Join` | Join 的多错误树要交给 errors.Is 遍历 |

下面这段把等值比较失效、`errors.Is` 命中与 Join 的行为跑成确定输出：

```go
package main

import (
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("not found")

func load(id string) error {
	return fmt.Errorf("加载 %s: %w", id, ErrNotFound)
}

func main() {
	err := load("u1")
	fmt.Println("等值比较 =", err == ErrNotFound)
	fmt.Println("errors.Is =", errors.Is(err, ErrNotFound))
	fmt.Println("errors.Unwrap =", errors.Unwrap(err))

	joined := errors.Join(ErrNotFound, errors.New("timeout"))
	fmt.Println("Is 命中子树 =", errors.Is(joined, ErrNotFound))
	fmt.Println("Unwrap 对 Join 返回 nil =", errors.Unwrap(joined) == nil)

	fmt.Println("链上文本 =", err.Error())
}
```

<OutputBox title="运行结果" output="等值比较 = false\nerrors.Is = true\nerrors.Unwrap = not found\nIs 命中子树 = true\nUnwrap 对 Join 返回 nil = true\n链上文本 = 加载 u1: not found" />

### 版本差异与适用边界

| 版本 | 变化 | 适用边界 |
| --- | --- | --- |
| Go 1.13 | 引入 `%w`、`errors.Is` / `errors.As` / `errors.Unwrap` | 之前只能比字符串或自己写包装类型 |
| Go 1.20 | 引入 `errors.Join` 与 `Unwrap() []error` 多错误协议 | 多个错误并列，`errors.Unwrap` 不认它 |
| Go 1.20 | `fmt.Errorf` 允许多个 `%w` | 一次挂多个根因 |
| 全版本 | `error` 只是 `Error() string` 接口 | 与异常机制无关 |

适用边界还有一条：`errors.As` 的第二个参数必须是指向目标类型的指针（如 `var pe *os.PathError; errors.As(err, &pe)`），传值会在运行时 panic；自定义错误类型若实现了 `Is(error) bool`，可以自己定义「什么算同一类错误」。

### 与 L3 底层、L10 实战的呼应

[第 030 讲](/core/interface-internals) 会说明 `error` 同样是接口，typed nil 的坑在这里一样成立——失败时返回 `(*MyError)(nil)` 会让 `err != nil` 为真；[第 057 讲](/engineering/error-design) 把本讲划清的边界落地成错误码、错误栈与统一响应。实战里，[第 112 讲](/projects/kv-2-network) 把哨兵错误映射成协议错误码而不泄露内部文案，[第 107 讲](/projects/data-log-etl) 用错误类别决定重试还是丢弃，[第 118 讲](/projects/ai-gateway) 需要把多家供应商的错误归一化成同一套可判定类型。

## 动手练一练

<Exercise title="用 %w 保住根因" task="补全下面的 wrap 函数：它接收一个 error，返回带上下文的新错误，但必须让外层的 errors.Is(err, ErrNotFound) 仍然为 true。想一想格式化动词选哪个、判定又该用哪个函数。" solution="func wrap(err error) error {\n    return fmt.Errorf(`加载用户: %w`, err)\n}\n// 外层判定：errors.Is(err, ErrNotFound)" :hints='["包装要用能保留 Unwrap 链的动词，不是 %v。","判定用 errors.Is，它会沿链向下找目标哨兵。"]' explain="%w 让 fmt.Errorf 生成一个实现了 Unwrap 的包装错误；errors.Is 沿这条链逐层比较，于是最底下的 ErrNotFound 依然能被认出来。" />

## 随堂检测

<Quiz no="020-1" q="关于 fmt.Errorf 的 %w，下面哪项说法是正确的？" :options='["%w 会把错误转成字符串，链到此为止","%w 生成一个实现 Unwrap 的包装错误，可被 errors.Is 回溯","%w 与 %v 完全等价，只是写法更短","%w 只能用于自定义错误类型，不能用于标准库错误"]' answer="1" explain="%w 的核心作用是保留可回溯的链；%v 只是把错误文本拼进来，链断掉。errors.Is 与 errors.As 都依赖这条链。" />

<Predict no="020-2" q="若把第二段代码里两处 %w 都改成 %v，errors.Is(err, os.ErrNotExist) 会输出什么？" :options='["true","false","编译报错","panic"]' answer="1" explain="改成 %v 后不再有 Unwrap 链，errors.Is 只能比较最外层那个字符串错误，与 os.ErrNotExist 不匹配，因此返回 false。代码照常编译，这也正是这个坑隐蔽的地方。" />

## 一页纸速记

<KnowledgeCard :points='["error 是实现了 Error() string 的接口值，成功返回 nil","中间层用 fmt.Errorf 配 %w 加上下文，形成错误链","判定类别用 errors.Is，取结构化字段用 errors.As","哨兵错误用包级变量复用，自定义类型可实现 Unwrap"]' :pitfalls='["把 %w 写成 %v 或在链中途重建错误，根因会丢失","用字符串匹配或等值比较包装后的错误，看似能用实则脆弱","每一层都打日志，同一故障重复刷屏"]' summary="加上下文而不丢根因，靠 %w 与 errors.Is 这一对配套动作。" />

## 小结与下一讲

- 错误是值：显式返回、显式判空，函数签名本身就说明了「可能失败」。
- 包装用 `%w`、判定用 `errors.Is` 与 `errors.As`，二者配套才能让链不断。
- 内部靠链和类型分类，业务错误码属于对外契约，留到工程化阶段处理。

下一讲我们解决另一个「收尾」问题：资源怎么保证被释放、程序遇到真正不该继续的情况又该怎么兜底。

[下一讲：defer / panic / recover](/basics/defer-panic)
