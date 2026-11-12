---
title: 第 057 讲 · 错误码、错误栈与统一响应
description: 设计前后端共用的错误契约，掌握错误分类与错误码、错误栈封装、内部错误到 HTTP/gRPC 状态映射与不泄露细节
---

<StageHeader stage="L5 · 工程化与质量" level="★★★★☆" time="45 分钟" goal="设计前后端都能用的错误契约" prereq="第 056 讲 · 配置：flag / env / viper / 配置中心" />

# 第 057 讲 · 错误码、错误栈与统一响应

> `errors.Is` 与 `%w` 解决了「程序内部怎么判定错误」，但一个对外服务还要回答另外三个问题：客户端怎么稳定识别错误类别、线上怎么定位是哪一行抛的、以及哪些信息绝对不能返回给调用方。这一讲把内部错误收敛成一份契约，重点不是多包一层，而是划清「给机器判定的码」与「给人看的提示」之间的边界。

## 这一讲你会得到什么
<CheckList id="goal-57" title="本讲目标" :items='["用错误码给错误分类，并用 errors.As 稳定提取","在错误产生处封装一次调用位置，保留错误链可判定性","把内部错误映射成统一响应，做到对外不泄露、对内可观测"]' />

## 1. 先建立直觉

把错误处理想成医院的分诊台：病人（底层错误）带着一堆私密病历进来，分诊台先贴一个类别标签（错误码），再写一句可以给家属听的说明（对外消息），而详细病历（SQL、堆栈、内网地址）只留在病历柜（日志）里。三件事分开，才不会既泄露隐私、又让排查时什么都查不到。

<ConceptDiagram type="error-chain" title="错误链：包装上下文但保留可判定性" />

关键是 `%w` 与 `errors.Is / As` 的搭配：每往上层走一步就包一层上下文，但底层错误始终留在链上，调用方既能读到人类可读的说明，又能按类别做机器判定。错误码就是这个「类别标签」，它必须比字符串稳定——字符串会改错别字，整数码不会。

## 2. 语法与规则

第一块代码定义最小可用的错误码与自定义错误类型，注意 `Unwrap` 让错误链不断。

```go
// 业务错误码：让调用方按类别判定，而不是比字符串
package main

import (
	"errors"
	"fmt"
)

// Code 是稳定的对外错误类别
type Code int

const (
	CodeOK       Code = 0
	CodeNotFound Code = 1001
	CodeInvalid  Code = 1002
	CodeInternal Code = 1500
)

// AppError 携带错误码与可对外展示的消息
type AppError struct {
	Code  Code
	Msg   string
	cause error
}

func (e *AppError) Error() string { return e.Msg }
func (e *AppError) Unwrap() error { return e.cause }

// Wrap 在底层错误之外附上一层类别与上下文
func Wrap(c Code, msg string, cause error) error {
	return &AppError{Code: c, Msg: msg, cause: cause}
}

// CodeOf 沿错误链找出最近的 AppError 类别
func CodeOf(err error) Code {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return CodeInternal
}

func main() {
	base := errors.New("sql: no rows")
	err := Wrap(CodeNotFound, "订单不存在", base)
	fmt.Println("错误码:", CodeOf(err))
	fmt.Println("仍可判定原始错误:", errors.Is(err, base))
}
```

<OutputBox title="运行结果" output="错误码: 1001\n仍可判定原始错误: true" note="errors.As 负责取类别，errors.Is 负责追原始错误，两者靠 Unwrap 同时成立。" />

第二块代码在错误产生处记录一次调用位置。真实项目里这一步通常由统一的错误封装函数完成，避免每层都手写堆栈。

```go
// 错误栈：在错误产生处记录一次调用点，便于线上定位
package main

import (
	"fmt"
	"runtime"
)

// StackError 在包装时附带一次调用点信息
type StackError struct {
	msg   string
	frame string
}

func (e *StackError) Error() string {
	return e.msg + " @ " + e.frame
}

// here 跳过自身与包装函数，记录真正触发错误的调用者
func here() string {
	pcs := make([]uintptr, 1)
	n := runtime.Callers(3, pcs)
	if n == 0 {
		return "unknown"
	}
	return runtime.FuncForPC(pcs[0]).Name()
}

func NewStack(msg string) error {
	return &StackError{msg: msg, frame: here()}
}

func main() {
	fmt.Println(NewStack("读取配置失败"))
}
```

```text
读取配置失败 @ main.main
```

第三块是最容易漏掉的一环：内部错误到统一响应。客户端只看到安全提示，细节全部进日志。

```go
// 统一响应：内部错误映射到对外状态，细节只进日志
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
)

type Code int

const (
	CodeNotFound Code = 1001
	CodeInternal Code = 1500
)

// Response 是前后端共用的响应契约
type Response struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

// statusOf 把内部错误码映射成 HTTP 状态码
func statusOf(c Code) int {
	switch c {
	case CodeNotFound:
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

// writeErr 只把安全提示写给客户端，内部细节写日志
func writeErr(w http.ResponseWriter, c Code, logMsg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusOf(c))
	msg := "请求失败"
	if c == CodeNotFound {
		msg = "资源不存在"
	}
	_ = json.NewEncoder(w).Encode(Response{Code: int(c), Msg: msg})
	fmt.Println("internal log:", logMsg)
}

func main() {
	rec := httptest.NewRecorder()
	writeErr(rec, CodeNotFound, "sql: no rows in order table")
	fmt.Println("状态码:", rec.Code)
	fmt.Println("响应体:", rec.Body.String())
}
```

```text
internal log: sql: no rows in order table
状态码: 404
响应体: {"code":1001,"msg":"资源不存在"}
```

<LineByLine title="逐行看错误码到状态的映射" code="func statusOf(c Code) int {\n    switch c {\n    case CodeNotFound:\n        return 404\n    default:\n        return 500\n    }\n}" :notes='["映射函数只接收错误码，不碰内部错误对象","按类别分支，稳定且易扩展","未找到映射为 404","返回对外状态","未知类别走兜底","统一归 500，绝不给 200","switch 闭合","函数闭合"]' />

### 错误码、错误链、统一响应为什么要分开

这三层各自回答一个不同的问题，合成一层就会顾此失彼。

错误码用整数而不是字符串，是因为它给机器判定：文案会改错别字、会做多语言、会被不同人各写一份，客户端若靠「资源不存在」这几个字分支，一次文案调整就能让全线失效；整数码一旦发布就是契约。`%w` 与 `Unwrap` 保留错误链，则是因为既要给人看上下文、又要让机器追根因：如果把底层错误用字符串拼进 message，`errors.Is(err, sql.ErrNoRows)` 立刻失效，调用方再也无法区分「没找到」和「数据库挂了」，只能一律当内部错误。`errors.As` 沿链取出最近的自定义类型拿类别，`errors.Is` 沿链追到原始哨兵，两者靠 `Unwrap` 同时成立——第一块代码的输出正是这个结论。

调用位置只在错误产生处记一次，是因为 `runtime.Callers` 有成本，每层都包栈会让同一故障在日志里刷出十几行，反而淹没根因；其余层用 `%w` 附上下文就够了。映射收敛到边界一处（`statusOf`），是因为散落在各 handler 里必然出现「新加的 handler 忘了映射，把 `err.Error()` 原样返回」或「未知类别漏出 200」；集中后的 `default` 分支能兜住所有未来新增的码。最后，「对外安全提示」与「对内详细日志」必须分开：响应体会被客户端、网关、CDN、第三方 SDK 记录，SQL 文本、内网地址、堆栈路径一旦返回就收不回，还把内部结构递给了攻击者；可排查又离不开这些细节，所以它们只进日志并关联 `trace_id`（呼应第 055 讲）。

| 做法 | 客户端能判定吗 | 线上能定位吗 | 对外安全吗 | 代价 |
| --- | --- | --- | --- | --- |
| 直接回显 err.Error() | 靠猜文案 | 日志里往往没留 | 否，细节全泄 | 零成本，但两头不讨好 |
| 字符串错误码 | 文案一改就失效 | 勉强 | 取决于实现 | 无稳定性保证 |
| 每层都包栈 | 能 | 被十几行堆栈淹没 | 取决于实现 | 日志噪声与运行时开销 |
| 整数码 + %w + 边界映射 | 能 | 能，码与调用点齐全 | 能 | 需维护码表与映射函数 |

### 可验证实验：错误链还能不能判定

下面这段程序把「可判定性丢失」这件事变成两行确定的输出：

```go
package main

import (
	"errors"
	"fmt"
)

var errNotFound = errors.New("record not found")

func main() {
	wrapped := fmt.Errorf("查询订单: %w", errNotFound)
	lost := fmt.Errorf("查询订单: %v", errNotFound)
	fmt.Println("用 %w 包装仍可判定:", errors.Is(wrapped, errNotFound))
	fmt.Println("用 %v 拼接后仍可判定:", errors.Is(lost, errNotFound))
}
```

```text
用 %w 包装仍可判定: true
用 %v 拼接后仍可判定: false
```

逐字段看：`%w` 让返回的错误带上了 `Unwrap()`，`errors.Is` 能沿链一路追到 `errNotFound`，所以为 true；`%v` 只把文字拼进 message，链在这里断了，于是为 false。第二种写法就是「错误被吞掉」的温和形式——错误其实没丢，上层日志照样能看到那句话，但类别信息没了，客户端和监控只能把它当统一内部错误处理，重试、降级、告警全都失去依据。Go 1.13 起才有 `%w`，Go 1.20 起 `errors.Join` 支持一次包装多个错误，版本差异会直接改变判定行为，这也是 CI 要锁定 Go 版本的原因之一。

## 3. 深入一步

错误码段位一旦定下就要长期稳定，新增类别往段内加，别复用旧码：

| 类别 | 码段 | 对外状态 | 对外信息 |
| --- | --- | --- | --- |
| 参数错误 | 1000–1099 | 400 | 指出哪个字段不合法 |
| 未找到 / 冲突 | 1100–1199 | 404 / 409 | 资源不存在 |
| 未授权 | 1200–1299 | 401 / 403 | 请重新登录 |
| 内部错误 | 1500 起 | 500 | 统一「请求失败」 |

<Term word="错误码" text="给错误类别分配的稳定整数标识，供客户端与监控系统做机器判定，不应随提示文案变化。" />

<Callout type="warn" title="易错点：不泄露细节" text="把 err.Error() 直接写进 HTTP 响应，会把 SQL 文本、内网地址、堆栈路径一起送给调用方；而另一类反模式是内部日志里什么都没记。两者叠加的结果最糟：既泄露了，又查不到。" />

### errors.Is / As 在源码里怎么遍历 Unwrap 树：%w 与 %v 的结构差异

上一节的实验只给了 `true / false`，这里把「链为什么断」追到类型结构。`fmt.Errorf` 遇到 `%w` 时（`src/fmt/errors.go`）构造的是 `*fmt.wrapError`，它有两个字段 `msg string` 与 `err error`，并实现了 `Unwrap() error` 把 `err` 交出去；遇到 `%v` 时只把文字拼进 message，返回的是 `*errors.errorString`——一个只有 `Error() string` 方法、没有 `Unwrap` 的普通错误。所以 `%w` 与 `%v` 的差别不是措辞，而是**返回值的动态类型上有没有 `Unwrap` 方法**，这是编译期就定死的结构，不是运行时能补回来的。

`errors.Is`（`src/errors/wrap.go`）本身是一个循环：每轮先比较 `err == target`，再若当前错误实现了 `Is(error) bool` 就调用它，然后尝试 `Unwrap() error` 向下走一层，或 `Unwrap() []error`（`errors.Join` 的多错误链）逐个子节点走，直到链尾返回 nil。`errors.As` 用反射沿**同一条链**对每个节点做类型可赋值判断，命中就把值写进 target。因此第二参数必须是非 nil 指针，且指向实现了 `error` 的类型或接口，否则运行时 panic——这不是编译期能宽松处理的边界。

由此推出几条实践结论，每条都可证伪：包一层上下文用 `%w` 才让 `Is/As` 能沿链追到哨兵错误，用 `%v` 链当场断掉；自定义错误类型只要实现 `Unwrap` 就能接进同一条链（第一块代码的 `AppError` 正是如此）；`errors.Join` 之所以能被 `Is` 遍历，靠的是 `Unwrap() []error` 这个分支；`errors.As` 的 target 约束是运行时强制，传值或 nil 指针都会 panic。

可复现实验（Go 1.23.8 实测）。把动态类型、Unwrap 结果、Is/As 判定与 panic 一起打出来：

```go
package main

import (
	"errors"
	"fmt"
)

var errNotFound = errors.New("record not found")

type AppError struct {
	Code  int
	cause error
}

func (e *AppError) Error() string { return "app error" }
func (e *AppError) Unwrap() error { return e.cause }

func main() {
	base := errNotFound
	wrapped := fmt.Errorf("查询订单: %w", base)
	lost := fmt.Errorf("查询订单: %v", base)
	joined := errors.Join(base, fmt.Errorf("另一个错误"))

	fmt.Printf("wrapped 动态类型 = %T\n", wrapped)
	fmt.Printf("lost    动态类型 = %T\n", lost)
	_, ok := wrapped.(interface{ Unwrap() error })
	fmt.Println("wrapped 实现 Unwrap() error =", ok)
	fmt.Println("errors.Unwrap(wrapped) =", errors.Unwrap(wrapped))
	fmt.Println("errors.Unwrap(lost)    =", errors.Unwrap(lost))
	fmt.Println("errors.Is(wrapped, base) =", errors.Is(wrapped, base))
	fmt.Println("errors.Is(lost, base)    =", errors.Is(lost, base))
	fmt.Println("errors.Is(joined, base)  =", errors.Is(joined, base))

	var ae *AppError
	chain := fmt.Errorf("外层: %w", &AppError{Code: 1001, cause: base})
	fmt.Println("errors.As(chain, &ae) =", errors.As(chain, &ae), "code =", ae.Code)

	func() {
		defer func() { fmt.Println("As 第二参数传非指针:", recover()) }()
		_ = errors.As(chain, AppError{})
	}()
}
```

```text
wrapped 动态类型 = *fmt.wrapError
lost    动态类型 = *errors.errorString
wrapped 实现 Unwrap() error = true
errors.Unwrap(wrapped) = record not found
errors.Unwrap(lost)    = <nil>
errors.Is(wrapped, base) = true
errors.Is(lost, base)    = false
errors.Is(joined, base)  = true
errors.As(chain, &ae) = true code = 1001
As 第二参数传非指针: errors: target must be a non-nil pointer
```

逐字段看：前两行用 `%T` 打印动态类型，直接证明 `%w` 得到 `*fmt.wrapError`、`%v` 得到 `*errors.errorString`；`wrapped 实现 Unwrap() error = true` 是链式结构存在的证据；`errors.Unwrap(wrapped)` 剥出 `record not found`，而 `lost` 那行是 `fmt.Println` 对 nil error 的打印形式 `<nil>`，说明链在这里为空；`Is` 三行分别给出「可追」「已断」「`errors.Join` 的多错误链仍可追」；`As` 沿链找到 `*AppError` 并取出 `Code = 1001`；最后一行是 `As` 的运行时约束，传非指针 target 直接 panic 并给出明确消息。**机制上不这么做会发生什么**：把 `%w` 写成 `%v`，`CodeOf` 提取不到类别、`errors.Is(err, sql.ErrNoRows)` 全部失效，客户端只能把「没找到」和「数据库挂了」一并当内部错误——上一节那起吞掉错误的故障，根子就在这个结构差异上。

**边界与失效条件**：`errors.Is` 对 `Unwrap` 链没有显式环检测，自定义错误若让 `Unwrap` 指回自身会死循环（`fmt.wrapError` 的单向包装不会成环）；当前错误若实现了 `Is(error) bool`，该自定义判定优先于 `==` 比较；链只沿 `Unwrap` 单向向下、不回溯，也不检查「错误是否真的被上层读取过」。`fmt.wrapError` 是未导出类型，不能自己构造，只能靠 `%w` 生成——所以错误链的正确性完全系于包装时那一个动词。机制能证明「链可判定」，证明不了「错误分类是否合理」，后者仍是码表设计的事。

**版本前提**：`%w` 与 `errors.Is/As` 自 Go 1.13 起可用，`errors.Join`（`Unwrap() []error`）自 Go 1.20 起才有，版本差异会直接改变判定行为，CI 才要锁 Go 版本；本实验在 Go 1.23.8（darwin/arm64）实测。第 020 讲展开错误链的基础用法，本讲补的是它为什么成立。

### 一个 `%w` 还是两个：`wrapError` / `wrapErrors` / `joinError` 与自定义 `Is` 的优先级

上一节把 `%w` 与 `%v` 的差别追到了「返回值动态类型上有没有 `Unwrap` 方法」，但那里只看了单个 `%w`。Go 1.20 起 `fmt.Errorf` 允许一条格式串里写多个 `%w`，`errors.Join` 也能一次合并多个错误——这时「链」不再是一条线，而是一棵树。类型不同、遍历方式不同，把这三种搞混会直接判错类别。

先看 `src/fmt/errors.go` 的 `Errorf`：它按本次捕获到的 `%w` 个数 `len(p.wrappedErrs)` 分三种返回（约 28–49 行）：

| `%w` 个数 | 动态类型 | 结构 | Unwrap 形态 |
| --- | --- | --- | --- |
| 0 | `*errors.errorString` | 只有 `msg` | 无 |
| 1 | `*fmt.wrapError` | `msg string` + `err error` | `Unwrap() error` |
| ≥2 | `*fmt.wrapErrors` | `msg string` + `errs []error` | `Unwrap() []error` |

`src/errors/join.go` 的 `errors.Join` 则是第四种：过滤掉 nil 后构造 `*errors.joinError{errs []error}`，同样实现 `Unwrap() []error`（60 行），`Error()` 把各子错误用换行拼起来（44 行）。注意 `fmt.Errorf("...%w...%w")` 会**保留格式化后的完整消息文本**，而 `errors.Join` 的消息是各子错误文本的拼接——两者都能被 `Is` 遍历，但 `err.Error()` 长得很不一样。

这直接推翻一个常见假设：`errors.Unwrap(err)` 只认 `Unwrap() error`。实验（Go 1.23.8，darwin/arm64）：

```go
package main

import (
	"errors"
	"fmt"
)

func main() {
	e1, e2 := errors.New("E1"), errors.New("E2")

	one := fmt.Errorf("one: %w", e1)
	many := fmt.Errorf("a=%w b=%w", e1, e2)
	joined := errors.Join(e1, e2)

	fmt.Printf("单 %%w 类型 = %T\n", one)
	fmt.Printf("多 %%w 类型 = %T\n", many)
	fmt.Printf("Join   类型 = %T\n", joined)

	fmt.Println("errors.Unwrap(one)  =", errors.Unwrap(one))
	fmt.Println("errors.Unwrap(many) =", errors.Unwrap(many)) // 只认 Unwrap() error
	if u, ok := many.(interface{ Unwrap() []error }); ok {
		fmt.Println("many 实现 Unwrap() []error，子节点数 =", len(u.Unwrap()))
	}
	fmt.Println("errors.Is(many, E1) =", errors.Is(many, e1), " errors.Is(many, E2) =", errors.Is(many, e2))
}
```

```text
单 %w 类型 = *fmt.wrapError
多 %w 类型 = *fmt.wrapErrors
Join   类型 = *errors.joinError
errors.Unwrap(one)  = E1
errors.Unwrap(many) = <nil>
many 实现 Unwrap() []error，子节点数 = 2
errors.Is(many, E1) = true  errors.Is(many, E2) = true
```

逐字段：`%T` 打出三个不同的动态类型，证实它们不是同一个东西的别名；`errors.Unwrap(many)` 返回 `<nil>`（打印 nil error 的形式）——**多 `%w` 的错误用 `errors.Unwrap` 剥不出任何东西**，因为 `src/errors/wrap.go` 的 `Unwrap` 只对 `interface{ Unwrap() error }` 做断言，而 `wrapErrors` 只有 `Unwrap() []error`；要取子节点得自己断言 `interface{ Unwrap() []error }`，实测拿到 2 个。但 `errors.Is(many, E1)` 与 `errors.Is(many, E2)` 都是 true：`is()` 里的 `case interface{ Unwrap() []error }` 分支会遍历每个子节点递归判定（约 67 行），所以「`Is` 能走多分支」与「`Unwrap` 只走单链」并不矛盾，是两套方法。

由此推出一条可证伪的实践结论：写「沿链找第一个自定义错误」的工具时，若只用 `for { err = errors.Unwrap(err) }`，遇到多 `%w` 或 `errors.Join` 会**立刻断在 `<nil>`**，静默漏掉另一半子树。正确做法是照 `is()` 的形状，同时处理 `Unwrap() error` 与 `Unwrap() []error` 两类，并对后者递归。本讲第一块代码的 `CodeOf` 用的是 `errors.As`，`As` 源码（约 80 行起）同样带 `Unwrap() []error` 的深度优先遍历，所以它能穿透 `Join` 找到 `*AppError`——这是「用 `Is/As` 而不是自己拆链」的机制依据，不只是风格建议。

**自定义 `Is(error) bool` 的优先级**也能从 `is()` 的循环顺序精确读出（53–77 行）：每一轮先 `if targetComparable && err == target`，再 `if x, ok := err.(interface{ Is(error) bool }); ok && x.Is(target)`，最后才 `switch Unwrap`。也就是**相等比较 > 自定义 Is > 向下拆链**，且自定义 `Is` 在链的**每个节点**上都会被调用。实验：

```go
package main

import (
	"errors"
	"fmt"
)

var marker = errors.New("marker")

type custom struct{ tag string }

func (c *custom) Error() string { return "custom-" + c.tag }
func (c *custom) Is(target error) bool {
	fmt.Printf("  custom.Is 被调用: tag=%s target=%v -> %v\n", c.tag, target, target == marker)
	return target == marker
}

func main() {
	chain := fmt.Errorf("外层: %w", &custom{tag: "inner"})
	fmt.Println("errors.Is(chain, marker) =", errors.Is(chain, marker))
	fmt.Println("errors.Is(chain, errors.New(\"E1\")) =", errors.Is(chain, errors.New("E1")))
}
```

```text
  custom.Is 被调用: tag=inner target=marker -> true
errors.Is(chain, marker) = true
  custom.Is 被调用: tag=inner target=E1 -> false
errors.Is(chain, errors.New("E1")) = false
```

逐字段：`chain` 是 `*fmt.wrapError` 包着 `*custom`，`errors.Is` 第一轮在外层 `wrapError` 上判 `==`（不等）、它没有 `Is` 方法、`Unwrap() error` 走到 `*custom`；第二轮在 `*custom` 上先判 `==`（不等），再调它的 `Is`——日志证明 `custom.Is` **每个目标都被调用一次**，返回 true 时整条 `is()` 提前 `return true`，返回 false 才继续 `switch`（`*custom` 无 `Unwrap`，落 `default` 返回 false）。这解释了自定义 `Is` 的正确用法：它做「浅比较」（源码注释约 43 行明确要求「should only shallowly compare err and the target and not call Unwrap on either」），负责把「等价但不同实例」的错误判为同类（标准库里 `syscall.Errno.Is`、`os.PathError` 都这么用）；若在 `Is` 里又调 `errors.Is(err, target)` 或自己 `Unwrap`，就会和外层循环叠成递归。

**错误链成环**是这套实现的硬边界。`is()` 是一个不带 `visited` 集合、不带深度上限的 `for {}`：只要 `Unwrap()` 返回非 nil，它就一直转下去。让一个自定义错误的 `Unwrap()` 返回自身，链就成环，`errors.Is` 永不返回。实测（用 `-timeout` 兜底，避免真卡死）：

```go
package errloop

import (
	"errors"
	"testing"
)

type loopErr struct{ self error }

func (e *loopErr) Error() string { return "loop" }
func (e *loopErr) Unwrap() error { return e.self }

func TestIsOnCycle(t *testing.T) {
	e := &loopErr{}
	e.self = e // 自引用成环
	_ = errors.Is(e, errors.New("target"))
}
```

```bash
go test -run TestIsOnCycle -timeout=2s -v .
```

```text
=== RUN   TestIsOnCycle
panic: test timed out after 2s
	running tests:
		TestIsOnCycle (2s)
```

逐字段：`panic: test timed out after 2s` 说明测试进程被 `testing` 的超时机制强制打断，而不是 `errors.Is` 返回了 false；`running tests: TestIsOnCycle (2s)` 指认卡住的就是这个用例。标准库的 `fmt.wrapError` 单向包装不会成环（它只包传入的 `err`，不回指自己），`errors.Join` 的 `errs` 是构造时快照、也不会自我引用——**成环只能来自自定义 `Unwrap` 写错**（返回自身、或 A→B→A）。可证伪对照：把 `e.self` 改成 `nil`，`Unwrap()` 返回 nil，`is()` 的 `case Unwrap() error` 分支立刻 `return false`，测试瞬间通过。所以「`Unwrap` 必须严格朝向更底层、且最终到 nil」不是编码规范，是 `for {}` 循环的终止条件；用 `errors.Join` 拼装时也应避免把结果错误又作为自己的子错误传进去。

把这些接回本讲主题：错误码靠 `errors.As` 沿链提取，而链的形态由 `%w` 个数决定——单个 `%w` 是线、`Unwrap() error`；多个 `%w` 与 `errors.Join` 是树、`Unwrap() []error`；自定义 `Is` 是节点上的等价判定，优先级高于拆链；成环是自定义 `Unwrap` 让循环失去终止条件。设计错误契约时，「一个错误同时携带多个原因」用多 `%w` 或 `Join` 是对的，但要确认下游的 `CodeOf` / 日志提取用的是 `Is/As` 而不是只走 `errors.Unwrap` 的单链遍历，否则第二个原因会被静默丢掉。

**边界与失效条件**：`errors.As` 的第二参数仍必须是非 nil 指针且指向实现 `error` 的类型（第二版实验里已证 panic），多 `%w` 不改变这条；`isComparable := reflectlite.TypeOf(target).Comparable()` 为 false 时（target 是含 slice / map / func 的类型），`==` 那一轮被跳过，只能靠自定义 `Is`，这类 target 直接传给 `errors.Is` 容易判不出来；`errors.Join` 会过滤 nil 子错误，全 nil 时返回 nil；`fmt.Errorf` 的多 `%w` 是 Go 1.20 起的行为，1.13–1.19 只支持单个。

**版本前提**：本机 Go 1.23.8（darwin/arm64）实测；单个 `%w` / `errors.Is/As` 自 Go 1.13 起，多个 `%w` 与 `errors.Join`（`Unwrap() []error`）自 Go 1.20 起，`fmt.wrapErrors`、`errors.joinError` 的结构与 `is()` 无环检测的循环在 1.20–1.23 间稳定。第 020 讲展开错误链基础，本讲上一节补了 `%w`/`%v` 的结构差异，这里补的是「多个 `%w` 让链变树」、自定义 `Is` 的判定顺序与成环的终止条件；第 055 讲日志里的 `error` 字段、第 080 讲的错误标记都依赖同一份可被 `As` 提取的码表。

### 三条最常见的误解

**误解一：只要写了 `if err != nil` 就算处理了错误。** 反例：`if err != nil { return []Item{}, nil }` 把错误吞掉、还返回 nil 的 error，上层据此判定「成功但为空」，客户端拿到 200 加空列表，前端显示「暂无数据」；另一种极端是 `http.Error(w, err.Error(), 500)`，把 SQL、路径、堆栈原样回显。两种写法都让错误从可观测系统里消失或变成泄露。正确做法：按类别映射成响应、或包装后继续上抛，并在日志里留下错误码、调用点与 `trace_id`。

**误解二：错误码就是 HTTP 状态码，直接透传即可。** 反例：把 404 当业务码写死，gRPC 侧根本没有 HTTP 概念，内部重试与降级想按「是不是未找到」分支时也无从下手；反过来，一个 500 内部可能对应十几种业务原因，监控按状态码聚合只能看到一个数字。业务码是内部契约，在 HTTP 与 gRPC 边界各映射一次（`statusOf` / `codes.NotFound`），两边解耦才能各自演进。

**误解三：包装错误就是把 `err.Error()` 拼进新字符串。** 反例：`fmt.Errorf("失败: " + err.Error())` 会让 `errors.Is` 与 `errors.As` 全部失效，`CodeOf` 提取不到类别，客户端只看到一长串拼接文本；上一节的实验里 `%v` 那行 false 就是证据。正确做法是用 `%w` 保留链，`errors.As` 取类别、`errors.Is` 追根因，两者互不干扰。

## 4. 工程实践

一个错误从产生到返回，应该经过固定的四步，任何一步缺失都会让线上排查变难：

<FlowTimeline title="一个错误的对外旅程" :steps='[{"title":"底层错误","desc":"sql: no rows，含敏感细节","tone":"red"},{"title":"包装与分类","desc":"附错误码与调用位置，保留错误链","tone":"orange"},{"title":"边界映射","desc":"错误码转 HTTP 状态与统一响应","tone":"blue"},{"title":"可观测","desc":"细节写日志并关联 trace_id","tone":"green"}]' direction="horizontal" />

gRPC 侧同理：内部错误码映射到 `codes.NotFound`、`codes.InvalidArgument`、`codes.Internal` 等状态，而不是把字符串塞进 `status.Error` 的 message。可观测性上，日志里应当同时记下错误码、调用点与 `trace_id`，这样按码聚合能看趋势，按 trace_id 能还原单次请求。

错误码一旦对外发布就变成了契约，必须有一份随代码一起维护的码表：每个码的含义、对应的 HTTP 状态、客户端应当如何处理。新增码只追加不复用，废弃码保留一段时间并标注，否则客户端按旧码写的分支会在某次发版后悄悄失效。

<Compare title="直接回显与统一契约" :panels='[{"label":"写法 A · 直接回显","code":"// 反例：把内部错误原样返回\nhttp.Error(w, err.Error(), 500)\n// SQL、路径、堆栈全部外泄","note":"客户端看不懂，攻击者看得清，日志里却没留痕"},{"label":"写法 B · 统一契约","code":"// 正例：分类 + 统一响应 + 内部留痕\nwriteErr(w, CodeInternal, err.Error())\n// 客户端只见统一提示","note":"对外安全、对内可查，错误码还能被监控聚合"}]' />

还要警惕两个反模式：错误码只用字符串常量，客户端靠文案判断，改一个错别字就全线崩；或者每一层都重新包一个带栈的错误，日志里同一故障刷出十几行堆栈，反而看不出根因。调用位置记录一次就够，上下文靠 `%w` 逐层附即可。

### 一次真实故障：错误被吞掉，重试风暴放大了故障

现象：下游超时后，本服务接口返回 200 加空列表，前端显示「暂无数据」，同时上游的重试把已经吃紧的下游打得更瘫。排查路径：

1. 看监控发现下游错误率飙升，本服务错误率却是 0，说明错误根本没被上报；
2. 翻代码找到 `if err != nil { return []Item{}, nil }`——错误被吞，error 置成 nil，还顺手伪造了一份成功结果；
3. 调用方拿到「成功但为空」，判定为可重试，于是按退避策略反复请求，把局部超时放大成雪崩；
4. 根因不是某次写错，而是错误既没被分类、没被记录、也没被映射成状态，等于从客户端、日志、监控三个视角同时消失；
5. 修复：包装成 `CodeInternal` 继续上抛，边界映射成 503，日志记录错误码与 `trace_id`，并让重试只对可重试类别生效。

为什么四步流程缺一不可：错误码让监控能按类别聚合，统一响应让客户端能判断「该不该重试」，日志与 `trace_id` 让单次请求能还原，`%w` 链让根因可追。替代做法与代价也清楚——全局 `recover` 兜底会把所有可恢复错误升级成 500 且丢失类别，靠前端读文案判断则文案一改就失效，只在最外层打日志又拿不到产生处的调用点。

### 把错误契约接进 CI

```yaml
# .github/workflows/error-contract.yml（节选）
- uses: actions/setup-go@v5
  with:
    go-version-file: go.mod
- name: 错误契约用例
  run: go test -run 'TestCodeOf|TestStatusOf|TestNoLeak' ./... -count=1
- name: 禁止回显内部错误
  run: |
    if grep -R -nE 'err\.Error\(\)' ./internal --include='*.go' | grep -q 'http.Error'; then
      echo "发现直接回显内部错误"
      exit 1
    fi
```

第二条只是低成本兜底，真正守住「不泄露」的是 `TestNoLeak` 这类用例：断言响应体里不出现 SQL 关键字、文件路径、内网地址。版本锁定同样要紧：`%w` 自 Go 1.13 起可用、`errors.Join` 的多错误包装自 Go 1.20 起才有，`go-version-file: go.mod` 保证 `errors.Is/As` 的判定行为与本地一致；错误码表随代码入库、新增只追加不复用，CI 里的码表一致性检查才有稳定基准。

### 与 L10 实战工程结构的呼应

错误契约在 L10 是从第一讲就定下的：`/projects/kv-1-memory` 定义 `ErrNotFound` 之类的哨兵错误，`/projects/kv-2-network` 的协议帧错误、`/projects/net-shortlink` 的短链过期与不存在，都要在边界映射成不同状态；`/projects/micro-ecommerce` 的 gRPC 服务把业务码映射到 `codes.NotFound` / `codes.Internal`（呼应第 074 讲 gRPC 状态码）。再往前，第 055 讲日志里的 `error` 字段、第 080 讲链路追踪里的错误标记，都依赖同一份码表；第 056 讲的配置校验失败之所以能在启动期拦下，也是因为它把非法值当成了一个有类别的错误，而不是一句打印。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l5-engineering/lesson-057-error-design`](https://github.com/lfire/go/tree/main/examples/l5-engineering/lesson-057-error-design)。clone 仓库后进入 `examples/l5-engineering`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="实现错误码到状态的映射" task="补全 statusOf：CodeNotFound 返回 404，其余一律返回 500，保证未知错误不会意外暴露成 200。" :hints='["用 switch 对 Code 分支","只用 net/http 的状态码常量","default 分支要兜住所有未知类别"]' solution="func statusOf(c Code) int {\n    switch c {\n    case CodeNotFound:\n        return http.StatusNotFound\n    default:\n        return http.StatusInternalServerError\n    }\n}" explain="映射收敛在边界一处，未知类别统一归 500，避免新增错误码时忘记配状态而漏出成功响应。" />

## 随堂检测

<Quiz no="057-1" q="关于 errors.As 与错误码，哪项做法最稳妥？" :options='["用 err.Error() 的字符串判断类别","用稳定的整数码并靠 errors.As 提取","每个包各自定义一套码","把码拼进 message 让前端解析"]' answer="1" explain="整数码稳定、可判定；errors.As 能沿 %w 错误链找到自定义类型，不受文案改动影响。" />

<Predict no="057-2" q="CodeNotFound 经 statusOf 映射后，writeErr 写出的 HTTP 状态码是？" :options='["200","400","404","500"]' answer="2" output="404" explain="CodeNotFound 命中 switch 的第一个分支，返回 http.StatusNotFound，即 404。" />

## 一页纸速记

<KnowledgeCard :points='["错误码是给机器判定的稳定类别，不随文案变化","Unwrap 保留错误链，errors.As 取类别、errors.Is 追根因","调用位置只在错误产生处记录一次","边界统一映射状态与响应，内部细节只进日志并关联 trace_id"]' :pitfalls='["把 err.Error() 直接回显给客户端","日志里没有错误码与 trace_id，无法聚合也无法还原","每层都重复包栈，日志被同一故障刷屏"]' summary="好错误契约同时满足三件事：客户端能判定、线上能定位、对外不泄露。" />

## 小结与下一讲

- 错误码给错误分类，`errors.As` 沿 `%w` 链稳定提取，`errors.Is` 追原始错误。
- 调用位置在边界记录一次即可，避免重复堆栈刷屏。
- 统一响应把内部错误映射成安全状态；细节只进日志并关联 `trace_id`。

[下一讲：设计模式与 Functional Options / wire](/engineering/patterns-design)
