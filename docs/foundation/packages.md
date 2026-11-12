---
title: 第 11 讲 · 包、模块与依赖管理
description: 会组织代码、会加依赖、会处理版本，弄清包与模块的分工
---

<StageHeader stage="L1 · 编程地基" level="★★☆☆☆" time="30 分钟" goal="会组织代码、会加依赖、会处理版本" prereq="第 10 讲" />

# 第 11 讲 · 包、模块与依赖管理

> 程序一旦超过一个文件，就会冒出三个绕不开的问题：代码怎么分组、谁能用谁、依赖的版本怎么锁住。Go 用**包**回答前两个，用**模块**回答第三个，两者分工非常清晰。这一讲把 `go.mod` 里每一行、首字母大小写背后的可见性规则、`internal` 这道栅栏、语义化版本以及 `init` 的副作用讲明白，学完你就能把一个单文件小程序拆成能被团队接手的项目骨架。

## 这一讲你会得到什么

<CheckList id="L1-11" title="本讲目标" :items='["能说清包与模块各自负责什么，看懂 import 路径怎么拼出来","理解导出规则与 internal 目录的可见性边界","能读懂 go.mod 与 go.sum，知道语义化版本与 v2 路径规则","会用 replace 做本地替换，知道 init 与下划线导入的适用边界"]' />

## 1. 先建立直觉

把项目想成一本书：**包**是书里的一个个章节（同一目录下的 `.go` 文件属于同一个包），**模块**是整本书（一个 `go.mod` 管住它和它引用的所有外部书），`import` 路径是章节的地址，`go.sum` 是每一页的防伪校验码，而 `internal` 是书里带锁的抽屉——只有同一本书内部的章节能打开。包负责"代码怎么组织、谁能看见谁"，模块负责"整本书依赖哪些外部书、各自什么版本"。

<ConceptDiagram type="layout" title="包按职责分层，internal 把实现细节挡在外面" />

图上那条"依赖方向只能向下"是组织代码的核心约束：上层可以用下层，下层绝不能反过来引用上层，否则 Go 编译器会直接报**循环导入**错误。这不是风格建议，是语言层面的硬规则，逼着项目保持单向依赖。

## 2. 语法与规则

导入一个包之后，通过"包名点标识符"访问它导出的东西：

```go
package main

import (
	"fmt"
	"strings" // 导入路径就是标准库里的位置
)

func main() {
	title := "go modules"
	fmt.Println(strings.ToUpper(title))  // 通过包名访问导出函数
	fmt.Println(strings.Repeat("-", 3)) // 首字母大写才可跨包使用
}
```

<OutputBox title="运行结果" output="GO MODULES\n---" />

`import` 后面写的是**导入路径**（模块路径加目录），而代码里用的是**包名**；两者通常一致，但也可以不同，必要时用别名区分，例如 `str "strings"`。首字母大写决定标识符能否被别的包访问，这条第 03 讲见过的规则，正是包级可见性的全部机制——Go 没有 `public` 关键字。

第二段看包初始化。`init` 没有参数、没有返回值，在 `main` 之前自动执行：

```go
package main

import "fmt"

var version = "1.0" // 包级变量先完成初始化

func init() { // 包初始化时自动执行，先于 main
	version += "+init"
	fmt.Println("init 执行了")
}

func main() {
	fmt.Println("main 里的版本：", version)
}
```

<LineByLine code="var n int\nfunc init() {\n    n = 1\n}\nfunc main() {\n    fmt.Println(n)\n}" :notes='["包级变量最先初始化，这里先拿到零值 0","编译器自动调用 init，不需要谁显式调用","init 里给包级变量赋值为 1","main 在全部 init 之后才运行","因此打印出来的是被 init 改过的 1"]' />

<OutputBox title="运行结果" output="init 执行了\nmain 里的版本： 1.0+init" guess="true" />

执行顺序是：先初始化依赖包，再初始化本包的包级变量，然后按文件名字典序执行各 `init`，最后才进 `main`。一个包可以有多个 `init`，但别依赖它们之间的顺序写代码，那是给自己埋雷。

第三种导入写法最容易被忽略——只要副作用，不用包名：

```go
package main

import (
	"fmt"
	_ "net/http/pprof" // 只为触发该包 init 里的注册动作
)

func main() {
	fmt.Println("副作用导入已生效")
}
```

<OutputBox title="运行结果" output="副作用导入已生效" />

`net/http/pprof` 的 `init` 会把性能分析接口注册到默认的 HTTP 路由上，我们并不需要直接调用它的任何函数，所以用 `_` 空标识符导入。`database/sql` 的各家驱动也是同一套机制：靠 `init` 把自己注册进去，主程序只导入不调用。

## 3. 深入一步

<Term word="模块路径" text="go.mod 里 module 指令的值，是这个模块被外部 import 时的前缀，也是依赖解析的起点。" />

一个最小 `go.mod` 长这样，每一行都有明确职责：

```text
module example.com/hello-go

go 1.21

require (
    github.com/sirupsen/logrus v1.9.3
)
```

| 指令或文件 | 作用 |
| --- | --- |
| `module` | 声明本模块的导入路径前缀 |
| `go` | 声明源码所用的 Go 语言版本，影响语言特性与默认行为 |
| `require` | 声明直接依赖及其版本 |
| `go.sum` | 记录每个依赖版本的校验和，保证下载内容未被篡改 |
| `replace` | 把某个模块临时替换成本地目录或另一个来源 |
| `exclude` | 明确排除某个有问题的版本 |

版本号遵循<Term word="语义化版本" text="v主版本.次版本.补丁：主版本变化表示不兼容，次版本表示新增兼容功能，补丁表示修复。" />。有一条 Go 特有的硬规则：主版本升到 v2 及以上时，模块路径**必须**加上 `/v2` 后缀，这样 v1 与 v2 才能在同一个工程里共存。没打 tag 的提交会用伪版本（形如 `v0.0.0-时间戳-commit`）来精确锁定。

<Callout type="warn" title="版本差异与四个边界" text="第一，Go 1.16 起 Modules 已是默认，别再照老教程配 GOPATH；Go 1.18 加入 go.work 支持多模块联合开发，Go 1.21 加入 toolchain 指令声明推荐工具链（第 02 讲提过）。第二，v2 及以上不加路径后缀会直接解析失败。第三，replace 常用于本地联调或临时切到 fork，但只作用于主模块构建，临时的本地路径千万别提交进版本库。第四，internal 目录及其子目录只能被它父目录的祖先包导入，这是工具链强制执行的，不靠约定。" />

<Compare :panels='[{"label":"包 package","code":"同一目录一个包名\n控制标识符可见范围","note":"编译与组织单元，靠首字母大小写决定导出"},{"label":"模块 module","code":"一个 go.mod 统领\n锁定依赖版本","note":"版本与分发单元，管的是整本书引用了什么"}]' title="两个层面，别混为一谈" />

<FlowTimeline :steps='[{"title":"初始化依赖包","desc":"按 import 顺序递归完成","tone":"blue"},{"title":"包级变量","desc":"按声明顺序拿到零值或初值","tone":"green"},{"title":"执行 init","desc":"按文件名字典序，可有多个","tone":"orange"},{"title":"进入 main","desc":"全部初始化完成后才开始","tone":"purple"}]' direction="horizontal" />

## 4. 工程实践

组织代码时，包按**职责**划分而不是照搬分层套路：一个包讲清楚一件事，名字短、用单数、别叫 `util` 或 `common` 这种什么都往里塞的兜底包。能被外部用的才大写导出，实现细节一律小写；确实不想让其它模块碰的代码放进 `internal`，让工具链替你守住边界。

依赖管理上，几条习惯很值钱：改完 import 跑一次 `go mod tidy` 让 `go.mod` 与 `go.sum` 保持同步（第 04 讲讲过命令）；两者都要提交，CI 里可以加 `go mod verify` 复核校验和；升级依赖优先小步走，跨主版本要专门验证不兼容变更。临时 `replace` 联调完记得撤掉，否则别人拉下来会指向一个不存在的本地路径。

`init` 的克制尤其重要：只放注册、默认值这类轻量动作，别做 I/O、别连数据库、别依赖其它包的初始化顺序，更别把业务逻辑塞进去——它隐式执行、难测、难排查。包启动的完整顺序与全局状态的代价，阶段三第 35 讲会展开；这里先记住"init 能不用就不用"。

<Callout type="tip" title="循环导入怎么破" text="编译器报 import cycle 时，说明两个包互相引用了。通常不是加个空接口能绕过去的，而是该把共用的那部分抽到第三个更底层的包里，让依赖重新变成单向。顺着上面那张分层图想，比试着改 import 顺序有效得多。" />

## 动手练一练

<Exercise title="用 init 给包级变量赋初值" task="声明一个包级变量 count，用 init 把它设成 1，再在 main 里打印出来，观察 init 与 main 的先后。" :hints='["init 不写参数和返回值，写法是 func init()","包级变量声明在函数外，init 里直接赋值即可"]' solution="var count int\n\nfunc init() {\n    count = 1\n}\n\nfunc main() {\n    fmt.Println(count)\n}" explain="输出 1。包级变量先初始化，随后 init 自动执行把 count 设为 1，最后 main 才运行并打印。" />

## 随堂检测

<Quiz no="11-1" q="关于包与模块，下面哪一项说法是错误的？" :options='["同一目录下的 .go 文件属于同一个包","首字母大写的标识符才能被其它包访问","主版本升到 v2 时模块路径可以不加后缀","internal 目录的导入限制由工具链强制执行"]' answer="2" explain="v2 及以上必须在模块路径加 /v2 后缀，否则无法解析；这是 Go Modules 的硬规则。" />

<Predict no="11-2" q="第二段代码里，init 打印与 main 打印谁先出现？" :options='["main 先","init 先","两者顺序不确定","init 不会执行"]' answer="1" explain="包级变量初始化完成后执行 init，全部 init 结束才进 main，因此 init 那句先打印。" />

<FillBlank no="11-3" tpl="同一目录下的 .go 文件属于同一个 ___\n限制外部导入的目录名是 ___\n本地替换依赖用 go.mod 里的 ___ 指令" :answers='["包~package","internal","replace"]' hint="三空分别是组织单元、那个特殊的目录名、替换依赖的指令名。" explain="包是编译单元，internal 是工具链强制的可见性边界，replace 用于把依赖临时指向本地或 fork。" />

## 一页纸速记

<KnowledgeCard :points='["包管代码组织与可见性，模块管依赖版本；一个 go.mod 统领一个模块","import 路径是模块路径加目录，首字母大写才导出，internal 由工具链挡住外部导入","go.mod 的 module/go/require 与 go.sum 校验和共同保证可复现构建","语义化版本 v2 起必须加路径后缀；init 在 main 前按依赖与字典序自动执行"]' :pitfalls='["别有循环导入，抽公共部分到更底层的包","别把临时本地 replace 提交进版本库","别在 init 里做 I/O 或依赖执行顺序，也别写业务逻辑"]' summary="包决定谁能看见谁，模块决定依赖锁在哪一版，两条线合起来就是可维护的项目骨架。" />

## 小结与下一讲

- 包是编译与可见性单元，模块是版本单元；`import` 路径由模块路径加目录拼成。
- 导出靠首字母大小写，`internal` 由工具链强制隔离，`go.mod`/`go.sum` 锁住依赖版本。
- 语义化版本的 v2 路径规则、`replace` 的临时性、`init` 的隐式执行，都是工程上要守住的边界。

地基阶段的最后一块拼图，是把这些知识串成一个真正能用的命令行程序。

[下一讲：阶段综合：CLI 单位换算器](/foundation/capstone)
