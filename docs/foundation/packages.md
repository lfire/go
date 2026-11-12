---
title: 第 011 讲 · 包、模块与依赖管理
description: 会组织代码、会加依赖、会处理版本，弄清包与模块的分工
---

<StageHeader stage="L1 · 编程地基" level="★★☆☆☆" time="30 分钟" goal="会组织代码、会加依赖、会处理版本" prereq="第 010 讲" />

# 第 011 讲 · 包、模块与依赖管理

> 程序一旦超过一个文件，就会冒出三个绕不开的问题：代码怎么分组、谁能用谁、依赖的版本怎么锁住。Go 用**包**回答前两个，用**模块**回答第三个，两者分工非常清晰。这一讲把 `go.mod` 里每一行、首字母大小写背后的可见性规则、`internal` 这道栅栏、语义化版本以及 `init` 的副作用讲明白，学完你就能把一个单文件小程序拆成能被团队接手的项目骨架。

## 这一讲你会得到什么

<CheckList id="L1-11" title="本讲目标" :items='["能说清包与模块各自负责什么，看懂 import 路径怎么拼出来","理解导出规则与 internal 目录的可见性边界","能读懂 go.mod 与 go.sum，知道语义化版本与 v2 路径规则","会用 replace 做本地替换，知道 init 与下划线导入的适用边界"]' />

## 1. 先建立直觉

把项目想成一本书：**包**是书里的一个个章节（同一目录下的 `.go` 文件属于同一个包），**模块**是整本书（一个 `go.mod` 管住它和它引用的所有外部书），`import` 路径是章节的地址，`go.sum` 是每一页的防伪校验码，而 `internal` 是书里带锁的抽屉——只有同一本书内部的章节能打开。包负责"代码怎么组织、谁能看见谁"，模块负责"整本书依赖哪些外部书、各自什么版本"。

<ConceptDiagram type="layout" title="包按职责分层，internal 把实现细节挡在外面" />

图上那条"依赖方向只能向下"是组织代码的核心约束：上层可以用下层，下层绝不能反过来引用上层，否则 Go 编译器会直接报**循环导入**错误。这不是风格建议，是语言层面的硬规则，逼着项目保持单向依赖。

### 为什么要把"包"和"模块"分成两层

两个概念回答的是性质不同的问题，混成一层就会处处别扭。

**包**是编译与可见性单元：源码级、每个目录都有一个、由 `package` 声明决定，管的是"代码怎么分组、谁能看见谁"。**模块**是版本与分发单元：一个工程只有一份 `go.mod`，管的是"这整份代码依赖哪些外部代码、各自什么版本"。

顺着这个分工能推出两条硬规则。第一，依赖方向只能向下、不能循环导入：包是编译单元，编译器必须先编完被依赖的包才能编依赖方；一旦成环，就找不到一个可以最先编译的起点，于是 `import cycle` 是编译期错误而不是风格建议。第二，`internal` 必须由工具链强制：靠注释或团队约定一定会被绕过，只有编译器在解析 import 路径时按前缀判定，才能让"实现细节不外泄"成为保证。

替代方案的代价也清楚：若每个目录都能各自锁版本，版本信息会散落、冲突无从裁决；若只有一份全局清单、没有包这层，又表达不了"这个目录只对内可见"。书与章节的类比正好对应：一本书只有一份引用书目（模块），书里却有很多章节（包）。

### 三个能立刻跑的小实验

先看一份刚生成的 `go.mod` 到底写了什么：

```bash
go mod init example.com/try && cat go.mod
```

<OutputBox title="实验一：最小 go.mod" output="module example.com/try\n\ngo 1.23" />

`module` 是这个模块被外部 import 时的路径前缀，`go` 是它声明的语言版本；此刻没有 `require`，说明还没有引入任何外部依赖。命令能在任意目录成功，本身就验证了"模块不绑定 GOPATH 位置"。

再用两条 `go list` 把"包"与"模块"这两层分开看：

```bash
go list ./...
go list -m all
```

<OutputBox title="实验二：一条列包，一条列模块" output="example.com/try\nexample.com/try" />

第一条让工具链按**包**列出当前模块里能发现的包，输出的是导入路径；第二条按**模块**列出整棵依赖树，此刻只有主模块自己。一旦 `go get` 了第三方库，第二条会长出一串带版本号的依赖，而第一条仍旧只列你自己写的包——两层职责一眼可分。

第三条验证可复现构建的兜底检查：

```bash
go mod verify
```

<OutputBox title="实验三：核对已下载模块与校验和" output="all modules verified" />

它逐个把已下载模块的内容与 `go.sum` 里的记录比对，输出这句说明当前依赖与记录一致。删掉 go.sum 或篡改缓存后重跑，会得到不一致提示。这正是 [第 002 讲](/guide/setup) 说"go.sum 必须提交"的落地检查。

### 这些规则会在后面哪些讲反复出现

| 本讲埋下的点 | 后面在哪用到 |
| --- | --- |
| 包、import 与依赖方向 | [第 003 讲](/guide/hello) 的 import、[第 050 讲](/engineering/project-layout) 的项目结构 |
| `internal` 可见性 | [第 050 讲](/engineering/project-layout) 的目录分层 |
| `go.mod` 与可复现构建 | [第 002 讲](/guide/setup)、[第 004 讲](/guide/toolchain) 的 tidy、[第 060 讲](/engineering/cicd) |
| `init` 与包初始化顺序 | [第 035 讲](/core/startup) |
| 语义化版本与标签 | [第 090 讲](/distributed/docker) 的镜像标签 |

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

`import` 后面写的是**导入路径**（模块路径加目录），而代码里用的是**包名**；两者通常一致，但也可以不同，必要时用别名区分，例如 `str "strings"`。首字母大写决定标识符能否被别的包访问，这条第 003 讲见过的规则，正是包级可见性的全部机制——Go 没有 `public` 关键字。

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

<Callout type="warn" title="版本差异与四个边界" text="第一，Go 1.16 起 Modules 已是默认，别再照老教程配 GOPATH；Go 1.18 加入 go.work 支持多模块联合开发，Go 1.21 加入 toolchain 指令声明推荐工具链（第 002 讲提过）。第二，v2 及以上不加路径后缀会直接解析失败。第三，replace 常用于本地联调或临时切到 fork，但只作用于主模块构建，临时的本地路径千万别提交进版本库。第四，internal 目录及其子目录只能被它父目录的祖先包导入，这是工具链强制执行的，不靠约定。" />

<Compare :panels='[{"label":"包 package","code":"同一目录一个包名\n控制标识符可见范围","note":"编译与组织单元，靠首字母大小写决定导出"},{"label":"模块 module","code":"一个 go.mod 统领\n锁定依赖版本","note":"版本与分发单元，管的是整本书引用了什么"}]' title="两个层面，别混为一谈" />

<FlowTimeline :steps='[{"title":"初始化依赖包","desc":"按 import 顺序递归完成","tone":"blue"},{"title":"包级变量","desc":"按声明顺序拿到零值或初值","tone":"green"},{"title":"执行 init","desc":"按文件名字典序，可有多个","tone":"orange"},{"title":"进入 main","desc":"全部初始化完成后才开始","tone":"purple"}]' direction="horizontal" />

### 推导：为什么 v2 必须改模块路径

原文把"主版本升到 v2 要加 `/v2` 后缀"列为硬规则，它的理由是一条反证：

| 假设 | 会发生什么 |
| --- | --- |
| v1 与 v2 共用同一个导入路径 | 工程里 A 依赖 v1、B 依赖 v2 时，工具链只能为这个路径挑一个版本，另一方必然编译失败 |
| 把主版本写进路径 | v1 与 v2 在依赖图里成为两个不同的模块，可以共存，各自被不同的包引用 |
| 结果 | 不兼容的版本不再需要"二选一"，调用方按自己 import 的路径各取所需 |

所以 `/v2` 不是命名习惯，而是"把不兼容变成可共存"的机制。伪版本（时间戳加 commit 哈希）同理：没打 tag 的提交也能被精确锁定，否则 `go.sum` 就无从记录"当初用的是哪一次提交"。

### 推导：init 为什么能不用就不用

顺着执行方式往下推：init 隐式执行、没有参数、不能返回错误 → 调用点在源码里不可见，无法传参、无法把失败交回调用方 → 单测里难以隔离，出错时只能靠打印排查 → 若它还做 I/O、连数据库、或依赖其它包的初始化顺序，启动行为就会随环境与文件顺序漂移。所以只留"注册、默认值"这类无副作用的轻动作，注册类用途的下划线导入正是它的正当场景。包启动的完整顺序与全局状态的代价在 [第 035 讲](/core/startup) 展开。

### 零基础最容易带进来的四个误解

| 错误理解 | 为什么站不住 | 正确理解 |
| --- | --- | --- |
| 包名必须等于目录名 | 包名由文件里的 `package` 声明决定，导入路径是模块路径加目录 | 两者通常一致但不是必须，必要时用别名区分 |
| go.sum 记的是依赖的版本号 | 版本号在 `go.mod` 的 `require` 里 | go.sum 记的是每份模块内容的校验和 |
| 下划线导入会把包里的标识符都引进来，只是不起名 | 它只触发该包的 init，不引入任何可调用的标识符 | 用于注册类副作用，如 pprof 与数据库驱动 |
| 同一个目录里可以放两个不同包名的 .go 文件 | 编译器按目录组织包，发现两个包名就直接拒绝 | 同目录只能属于一个包，外部测试包是唯一例外 |

第三条与第二组实验相关，先看一段可编译的别名写法，弄清"导入路径"与"代码里用的名字"是两件事：

```go
package main

import (
	"fmt"
	str "strings" // 别名：代码里用 str，而不是路径末段 strings
)

func main() {
	fmt.Println(str.ToUpper("go modules"))
}
```

<OutputBox title="导入路径不等于代码里用的名字" output="GO MODULES" />

`import` 后面写的是路径，代码里用的是包名（或别名）。把 `str` 去掉也能跑，只是显式写出别名能让两者关系一目了然。

再撞一次"同目录两个包"的编译器边界：在一个目录里放 `package a` 与 `package b` 两个文件后执行 `go build`。

```bash
go build
```

<OutputBox title="按目录组织包，含两个包名即报错" output="found packages a (a.go) and b (b.go) in /path/to/dir" />

报错直接列出两个文件名与包名，说明工具链是以目录为单位认包的。想拆成两个包，正确做法是把文件移到两个不同目录，而不是改 import 顺序。

### 版本与平台边界

| 边界 | 说明 | 归属 |
| --- | --- | --- |
| Go 1.16 | Modules 成为默认，`GO111MODULE` 不再是必配项 | [第 002 讲](/guide/setup) |
| Go 1.18 | 加入 `go.work`，支持多模块联合开发 | 单模块练习用不到 |
| Go 1.21 | `go.mod` 支持 `toolchain` 指令；`go` 指令对语言特性的影响更明确 | [第 004 讲](/guide/toolchain) |
| 路径分隔符 | import 路径一律用正斜杠，与 Windows 文件系统的反斜杠无关 | 跨平台构建的前提之一 |
| `internal` 判定 | 基于导入路径前缀，由工具链强制，三平台行为一致 | 原文版本边界 |
| 缓存路径 | GOMODCACHE 的默认位置在 Windows 与 Unix 不同 | [第 002 讲](/guide/setup) |

## 4. 工程实践

组织代码时，包按**职责**划分而不是照搬分层套路：一个包讲清楚一件事，名字短、用单数、别叫 `util` 或 `common` 这种什么都往里塞的兜底包。能被外部用的才大写导出，实现细节一律小写；确实不想让其它模块碰的代码放进 `internal`，让工具链替你守住边界。

依赖管理上，几条习惯很值钱：改完 import 跑一次 `go mod tidy` 让 `go.mod` 与 `go.sum` 保持同步（第 004 讲讲过命令）；两者都要提交，CI 里可以加 `go mod verify` 复核校验和；升级依赖优先小步走，跨主版本要专门验证不兼容变更。临时 `replace` 联调完记得撤掉，否则别人拉下来会指向一个不存在的本地路径。

`init` 的克制尤其重要：只放注册、默认值这类轻量动作，别做 I/O、别连数据库、别依赖其它包的初始化顺序，更别把业务逻辑塞进去——它隐式执行、难测、难排查。包启动的完整顺序与全局状态的代价，阶段三第 035 讲会展开；这里先记住"init 能不用就不用"。

<Callout type="tip" title="循环导入怎么破" text="编译器报 import cycle 时，说明两个包互相引用了。通常不是加个空接口能绕过去的，而是该把共用的那部分抽到第三个更底层的包里，让依赖重新变成单向。顺着上面那张分层图想，比试着改 import 顺序有效得多。" />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l1-foundation/lesson-011-packages`](https://github.com/lfire/go/tree/main/examples/l1-foundation/lesson-011-packages)。clone 仓库后进入 `examples/l1-foundation`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="用 init 给包级变量赋初值" task="声明一个包级变量 count，用 init 把它设成 1，再在 main 里打印出来，观察 init 与 main 的先后。" :hints='["init 不写参数和返回值，写法是 func init()","包级变量声明在函数外，init 里直接赋值即可"]' solution="var count int\n\nfunc init() {\n    count = 1\n}\n\nfunc main() {\n    fmt.Println(count)\n}" explain="输出 1。包级变量先初始化，随后 init 自动执行把 count 设为 1，最后 main 才运行并打印。" />

## 随堂检测

<Quiz no="011-1" q="关于包与模块，下面哪一项说法是错误的？" :options='["同一目录下的 .go 文件属于同一个包","首字母大写的标识符才能被其它包访问","主版本升到 v2 时模块路径可以不加后缀","internal 目录的导入限制由工具链强制执行"]' answer="2" explain="v2 及以上必须在模块路径加 /v2 后缀，否则无法解析；这是 Go Modules 的硬规则。" />

<Predict no="011-2" q="第二段代码里，init 打印与 main 打印谁先出现？" :options='["main 先","init 先","两者顺序不确定","init 不会执行"]' answer="1" explain="包级变量初始化完成后执行 init，全部 init 结束才进 main，因此 init 那句先打印。" />

<FillBlank no="011-3" tpl="同一目录下的 .go 文件属于同一个 ___\n限制外部导入的目录名是 ___\n本地替换依赖用 go.mod 里的 ___ 指令" :answers='["包~package","internal","replace"]' hint="三空分别是组织单元、那个特殊的目录名、替换依赖的指令名。" explain="包是编译单元，internal 是工具链强制的可见性边界，replace 用于把依赖临时指向本地或 fork。" :explains='["同一目录下的源文件被编译成一个整体，共享包级标识符；包既是编译单元也是导入时的名字，目录不同便互不归属。","这个目录名由工具链强制生效，只有它父目录树内的代码可以导入，越界导入直接编译失败，用来圈住不对外暴露的内部实现。","该指令把某个依赖临时重定向到本地路径或 fork，方便联调未发布的改动；它只作用于当前主模块，不会污染依赖自身的声明。"]' />

## 一页纸速记

<KnowledgeCard :points='["包管代码组织与可见性，模块管依赖版本；一个 go.mod 统领一个模块","import 路径是模块路径加目录，首字母大写才导出，internal 由工具链挡住外部导入","go.mod 的 module/go/require 与 go.sum 校验和共同保证可复现构建","语义化版本 v2 起必须加路径后缀；init 在 main 前按依赖与字典序自动执行"]' :pitfalls='["别有循环导入，抽公共部分到更底层的包","别把临时本地 replace 提交进版本库","别在 init 里做 I/O 或依赖执行顺序，也别写业务逻辑"]' summary="包决定谁能看见谁，模块决定依赖锁在哪一版，两条线合起来就是可维护的项目骨架。" />

## 小结与下一讲

- 包是编译与可见性单元，模块是版本单元；`import` 路径由模块路径加目录拼成。
- 导出靠首字母大小写，`internal` 由工具链强制隔离，`go.mod`/`go.sum` 锁住依赖版本。
- 语义化版本的 v2 路径规则、`replace` 的临时性、`init` 的隐式执行，都是工程上要守住的边界。

地基阶段的最后一块拼图，是把这些知识串成一个真正能用的命令行程序。

[下一讲：阶段综合：CLI 单位换算器](/foundation/capstone)
