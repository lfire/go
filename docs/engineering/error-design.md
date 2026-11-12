---
title: 第 57 讲 · 错误码、错误栈与统一响应
description: 设计前后端共用的错误契约，掌握错误分类与错误码、错误栈封装、内部错误到 HTTP/gRPC 状态映射与不泄露细节
---

<StageHeader stage="L5 · 工程化与质量" level="★★★★☆" time="45 分钟" goal="设计前后端都能用的错误契约" prereq="第 56 讲 · 配置：flag / env / viper / 配置中心" />

# 第 57 讲 · 错误码、错误栈与统一响应

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

## 4. 工程实践

一个错误从产生到返回，应该经过固定的四步，任何一步缺失都会让线上排查变难：

<FlowTimeline title="一个错误的对外旅程" :steps='[{"title":"底层错误","desc":"sql: no rows，含敏感细节","tone":"red"},{"title":"包装与分类","desc":"附错误码与调用位置，保留错误链","tone":"orange"},{"title":"边界映射","desc":"错误码转 HTTP 状态与统一响应","tone":"blue"},{"title":"可观测","desc":"细节写日志并关联 trace_id","tone":"green"}]' direction="horizontal" />

gRPC 侧同理：内部错误码映射到 `codes.NotFound`、`codes.InvalidArgument`、`codes.Internal` 等状态，而不是把字符串塞进 `status.Error` 的 message。可观测性上，日志里应当同时记下错误码、调用点与 `trace_id`，这样按码聚合能看趋势，按 trace_id 能还原单次请求。

错误码一旦对外发布就变成了契约，必须有一份随代码一起维护的码表：每个码的含义、对应的 HTTP 状态、客户端应当如何处理。新增码只追加不复用，废弃码保留一段时间并标注，否则客户端按旧码写的分支会在某次发版后悄悄失效。

<Compare title="直接回显与统一契约" :panels='[{"label":"写法 A · 直接回显","code":"// 反例：把内部错误原样返回\nhttp.Error(w, err.Error(), 500)\n// SQL、路径、堆栈全部外泄","note":"客户端看不懂，攻击者看得清，日志里却没留痕"},{"label":"写法 B · 统一契约","code":"// 正例：分类 + 统一响应 + 内部留痕\nwriteErr(w, CodeInternal, err.Error())\n// 客户端只见统一提示","note":"对外安全、对内可查，错误码还能被监控聚合"}]' />

还要警惕两个反模式：错误码只用字符串常量，客户端靠文案判断，改一个错别字就全线崩；或者每一层都重新包一个带栈的错误，日志里同一故障刷出十几行堆栈，反而看不出根因。调用位置记录一次就够，上下文靠 `%w` 逐层附即可。

## 动手练一练

<Exercise title="实现错误码到状态的映射" task="补全 statusOf：CodeNotFound 返回 404，其余一律返回 500，保证未知错误不会意外暴露成 200。" :hints='["用 switch 对 Code 分支","只用 net/http 的状态码常量","default 分支要兜住所有未知类别"]' solution="func statusOf(c Code) int {\n    switch c {\n    case CodeNotFound:\n        return http.StatusNotFound\n    default:\n        return http.StatusInternalServerError\n    }\n}" explain="映射收敛在边界一处，未知类别统一归 500，避免新增错误码时忘记配状态而漏出成功响应。" />

## 随堂检测

<Quiz no="57-1" q="关于 errors.As 与错误码，哪项做法最稳妥？" :options='["用 err.Error() 的字符串判断类别","用稳定的整数码并靠 errors.As 提取","每个包各自定义一套码","把码拼进 message 让前端解析"]' answer="1" explain="整数码稳定、可判定；errors.As 能沿 %w 错误链找到自定义类型，不受文案改动影响。" />

<Predict no="57-2" q="CodeNotFound 经 statusOf 映射后，writeErr 写出的 HTTP 状态码是？" :options='["200","400","404","500"]' answer="2" output="404" explain="CodeNotFound 命中 switch 的第一个分支，返回 http.StatusNotFound，即 404。" />

## 一页纸速记

<KnowledgeCard :points='["错误码是给机器判定的稳定类别，不随文案变化","Unwrap 保留错误链，errors.As 取类别、errors.Is 追根因","调用位置只在错误产生处记录一次","边界统一映射状态与响应，内部细节只进日志并关联 trace_id"]' :pitfalls='["把 err.Error() 直接回显给客户端","日志里没有错误码与 trace_id，无法聚合也无法还原","每层都重复包栈，日志被同一故障刷屏"]' summary="好错误契约同时满足三件事：客户端能判定、线上能定位、对外不泄露。" />

## 小结与下一讲

- 错误码给错误分类，`errors.As` 沿 `%w` 链稳定提取，`errors.Is` 追原始错误。
- 调用位置在边界记录一次即可，避免重复堆栈刷屏。
- 统一响应把内部错误映射成安全状态；细节只进日志并关联 `trace_id`。

[下一讲：设计模式与 Functional Options / wire](/engineering/patterns-design)
