---
title: 第 004 讲 · 工具链全景：build / test / fmt / vet / mod
description: 掌握日常开发最常用的命令与排查手段，让工具替团队守住风格与质量
---

<StageHeader stage="L0 · 导学" level="★★☆☆☆" time="30 分钟" goal="掌握日常开发最常用的命令与排查手段" prereq="第 003 讲 · 第一行 Go 代码：从源码到可执行文件" />

# 第 004 讲 · 工具链全景：build / test / fmt / vet / mod

> 很多语言生态里，格式化、静态检查、依赖管理要靠一堆第三方工具拼起来，每个项目配法还不一样。Go 把这些全部做成了官方命令，装好工具链就有一套统一流水线。这一讲不要求背参数，重点是建立一张"什么时候该跑哪条命令"的地图：写代码时靠 `gofmt`，提交前靠 `vet` 与 `test`，依赖乱了靠 `go mod tidy`，重复代码靠 `go generate`，交付靠 `go build` 与交叉编译。

## 这一讲你会得到什么

<CheckList id="L0-04" title="本讲目标" :items='["能画出日常开发的命令地图，知道每条命令解决什么问题","能说清 gofmt 与 goimports 的分工，以及为什么 gofmt 没有配置项","能解释 go vet 抓的是编译能过、但运行时会出错的可疑写法","会跑 go mod tidy、go generate，并用 GOOS/GOARCH 做交叉编译"]' />

## 1. 先建立直觉

把工具链想成一条出厂流水线上的质检工位：`gofmt` 负责把每个零件摆得一模一样（统一外观），`go vet` 拿着检查表找"看着没问题、装上会出事"的可疑装配，`go test` 实际通电试运行，`go mod tidy` 清点配料表、把没用上的原料退回去，`go generate` 让机器自动生产那些重复零件，最后 `go build` 打包出厂。它们的共同点是不需要额外安装、不挑项目、全团队跑出来的结果一致。

<ConceptDiagram type="compile-link" title="工具链都围绕同一条编译流程工作" />

这张图解释了为什么 Go 的工具链能这么统一：所有命令共享同一套包解析、类型检查与编译前端，所以 `vet` 看到的类型信息和编译器看到的完全一致，不会出现"编辑器说不报错、CI 说报错"的割裂。

### 为什么 Go 把工具做成官方命令，而不是交给生态插件

别的语言里，格式化、静态检查、依赖管理往往各由第三方工具承担，每个项目装的版本和配置都不同，于是常出现"我本地能过、CI 过不了"。Go 把它们内置进 `go` 命令，好处不是"少装几个软件"，而是来自一个共同前提：所有命令共享同一套包解析、类型检查与编译前端。

顺着这个前提往下推：`go vet` 看到的类型信息和编译器看到的完全一致，所以不会出现"编辑器说不报错、CI 说报错"的割裂；`gofmt` 不需要安装、不读项目配置，因此 `gofmt -l .` 在任何一台装了 Go 的机器上结果一致，风格检查可以无条件放进 CI。再推一层，为什么 `gofmt` 连缩进用几个空格都不让配？因为一旦允许配置，每个团队、每个项目又会分叉，代码评审重新开始争论排版；把"可配置"这件事整个砍掉，才换得全生态一致。

代价也很清楚：官方工具只覆盖"大家能达成共识"的部分，个性化规则交给 `golangci-lint` 这类聚合器（[第 051 讲](/engineering/code-style)）。这条分工边界，比"Go 工具少"这种印象更值得记住。

### 三个能立刻跑的小实验

```bash
gofmt -l .
```

<OutputBox title="实验一：只列名字、不改文件" output="main.go" />

`-l` 有输出就说明还有文件没按官方风格排版，此时才需要 `gofmt -w` 去改。逐项看：`-l` 是 list，只打印文件名；输出为空表示全部合格；这正是把风格检查放进 CI 而不担心它悄悄改代码的原因。相对地，`gofmt -d main.go` 会把差异以 diff 形式打出来，适合先看清要改什么。

下面这段能编译、`gofmt` 也检查不出问题，但格式符和实参类型对不上——正好用来验证 `go vet` 抓的是哪一类问题：

```go
package main

import "fmt"

func main() {
	name := "Gopher"
	fmt.Printf("编号是 %d\n", name) // 故意用错格式符：能编译，但 vet 会报
}
```

```bash
go vet ./...
```

<OutputBox title="实验二：编译能过、运行会出错的可疑写法" output="./main.go:7:2: fmt.Printf format %d has arg name of wrong type string" />

这条输出分三段读：`./main.go:7:2` 是位置，`%d` 是期望整数的格式符，`wrong type string` 说明实际传进来的是字符串。程序确实能编译、能运行，只会打印出类似"编号是 %!d(string=Gopher)"的怪东西；`go vet` 在提交前就把这类漏网之鱼捞出来。把 `%d` 改成 `%s` 再跑一次 `go vet ./...`，输出为空即通过，一正一反刚好把"静态检查"的边界验证清楚。

```bash
go tool dist list | head -n 5
```

<OutputBox title="实验三：查交叉编译的目标清单（节选，Unix 下）" output="aix/ppc64\nandroid/386\nandroid/amd64\ndarwin/amd64\ndarwin/arm64" />

每行是 `GOOS/GOARCH` 的一个受支持组合，写构建脚本前查一眼比凭记忆可靠。`head` 是 Unix 管道命令，Windows 下没有，可以改成 `go tool dist list` 直接看全量再手动挑——这本身就提示了一个平台差异：命令行的写法并不跨平台，而 Go 源码与构建结果是跨平台的。

### 这些命令会在后面哪些讲反复出现

| 本讲埋下的点 | 后面在哪用到 |
| --- | --- |
| gofmt / goimports / vet / test 流水线 | [第 051 讲](/engineering/code-style) 的规则集与 CI 接入 |
| `go test ./...` | [第 052 讲](/engineering/unit-test) 的表驱动测试、[第 054 讲](/engineering/bench-fuzz) 的基准与模糊测试 |
| `go mod tidy` | [第 011 讲](/foundation/packages) 的依赖版本、[第 060 讲](/engineering/cicd) 的构建一致性 |
| `go generate` | [第 023 讲](/basics/reflect) 的代码生成、[第 073 讲](/microservice/protobuf) 的 protoc 生成 |
| GOOS / GOARCH 交叉编译 | [第 090 讲](/distributed/docker) 的多阶段构建 |
| 诊断类命令 | [第 059 讲](/engineering/profiling) 的 pprof 与 trace |

## 2. 语法与规则

先看日常最高频的几条命令，以及各自该在什么时机跑：

| 命令 | 解决什么 | 建议时机 |
| --- | --- | --- |
| `gofmt -w .` | 按官方风格重写源码排版 | 每次保存、提交前 |
| `goimports -w .` | 在 gofmt 之外自动增删 import | 保存时由编辑器调用 |
| `go vet ./...` | 静态检查可疑写法 | 提交前、CI |
| `go test ./...` | 跑全部包的单元测试 | 提交前、CI |
| `go mod tidy` | 补齐缺失依赖、删除无用依赖 | 增删 import 之后 |
| `go generate ./...` | 执行源码里的生成指令 | 生成代码需要更新时 |
| `go build ./...` | 编译全部包并产出二进制 | 发布、打镜像 |

`gofmt` 最反直觉的一点是：它**没有配置项**。缩进、空行、括号位置全由官方定死，这正是第 001 讲说的"风格统一到没什么可争论"落到实处的手段。下面这段就是 gofmt 之后该有的样子：

```go
package main

import "fmt"

func main() {
	total := 40 + 2
	fmt.Println("合计：", total)
}
```

<OutputBox title="运行结果" output="合计： 42" />

`goimports` 是 gofmt 的超集，额外帮你补上漏写的 import、删掉多写的 import。它不在官方工具链里，需要单独安装，但编辑器插件通常已内置。

第二段代码留一个纯粹的小函数，正好用来说明 `go test` 与 `go vet` 各看什么：

```go
package main

import "fmt"

func add(a int, b int) int { // 一个值得被单元测试覆盖的小函数
	return a + b
}

func main() {
	fmt.Printf("40 加 2 等于 %d\n", add(40, 2)) // 格式串与参数类型一致
}
```

<LineByLine code="total := add(40, 2)\nfmt.Println(total)" :notes='["调用 add 并把返回值存进新变量，add 是前面定义的纯函数","把这个结果打印出来；go test 可以单独给 add 写用例，不必启动整个程序"]' />

`go test ./...` 会自动寻找同目录下以 `_test.go` 结尾的文件并运行；`go vet` 则盯着另一类问题，比如 `fmt.Printf` 的格式串写 `%d` 却传了字符串——这种代码**能编译通过**，运行时却会打印出奇怪的东西。vet 的价值就在这里：把编译器的漏网之鱼提前捞出来。

<Compare :panels='[{"label":"gofmt","code":"gofmt -w main.go","note":"只重排缩进与空行，不改动任何语义，也不碰 import 列表"},{"label":"goimports","code":"goimports -w main.go","note":"先按 gofmt 格式化，再自动增删 import 分组"}]' title="两个格式化工具的分工" />

第三段代码演示 `go generate` 与交叉编译。源码里的 `//go:generate` 只是注释，不影响编译：

```go
package main

import (
	"fmt"
	"runtime"
)

//go:generate echo 真实项目常在这里调用 stringer 之类的生成器

func main() {
	fmt.Println("构建目标：", runtime.GOOS, runtime.GOARCH)
}
```

<OutputBox title="运行结果（平台相关）" output="构建目标： darwin arm64" />

执行 `go generate ./...` 时，工具链会扫描并执行这条注释里的命令；改了枚举或协议后重新生成一次，比手工维护重复代码可靠得多。而把 `GOOS`、`GOARCH` 换成别的值再 `go build`，就能得到另一个平台的可执行文件，无需那台机器的工具链。

## 3. 深入一步

<Term word="静态检查" text="不运行程序，只分析源码与类型信息来发现可疑写法，go vet 就是官方实现。" />

<Term word="可复现构建" text="源码加 go.mod 加 go.sum 在任何机器上都能构建出同一份产物，go mod tidy 是维护它的关键。" />

把常用命令串成一条流水线，提交前依次跑一遍，绝大多数低级问题都进不了代码库：

<FlowTimeline :steps='[{"title":"gofmt -l","desc":"列出还没格式化的文件，非空就打回","tone":"blue"},{"title":"go vet ./...","desc":"静态检查可疑写法","tone":"purple"},{"title":"go test ./...","desc":"跑全部单测，必要时加 -race","tone":"orange"},{"title":"go build ./...","desc":"编译通过才允许合并","tone":"green"}]' direction="horizontal" />

<Callout type="warn" title="版本差异与边界" text="gofmt 从 Go 1.19 起会重新排版文档注释，老项目升级后可能出现大面积 diff，属正常现象；go.mod 里的 toolchain 指令是 Go 1.21 才引入的；go vet 的检查项也会随版本增加，升级后冒出新的告警要先看清楚再修，不要直接忽略。另外 vet 只报可疑、不保证全对，而 go test 只覆盖你写了用例的部分，两者都不能替代代码评审。" />

`go mod tidy` 容易被忽略但很重要：它会扫描全部源码里的 import，把用到却没声明的依赖加进 `go.mod`，把声明了却没人用的删掉，并同步 `go.sum`。凡是大改过 import 之后，提交前都该跑一次，否则 CI 上的依赖状态和你本机不一致。

### 推导：go mod tidy 为什么不能省

"改完 import 跑一次 tidy"是结论，把它拆成两个集合就清楚了：集合 A 是全源码里实际出现的 import 路径，集合 B 是 `go.mod` 里 `require` 声明的模块。增删代码只改 A，不会自动改 B，于是两者会随时间漂移：A 里有、B 里没有时，CI 上解析依赖可能拿到不同版本甚至失败；B 里有、A 里没有时，多出来的依赖会进依赖图、进构建缓存，还可能带来不必要的许可证与供应链面。

`tidy` 做的事就是重新扫描全部源码、算出 A 与 B 的差集，把缺的补进 `go.mod`、把多的删掉，并同步 `go.sum`。由此还能推出一条 CI 习惯：把 `go mod tidy` 的结果与提交内容做比对（例如跑完看工作区是否干净），就能把"本机依赖状态和仓库不一致"这类问题挡在合并之前。它和 `go build` 的分工也不同——`go build` 只按现有 `go.mod` 编译，不会替你纠正这份清单。

### 零基础最容易带进来的五个误解

| 错误理解或写法 | 为什么站不住 | 正确理解 |
| --- | --- | --- |
| 在 CI 里放 `gofmt -w .` 就能保证风格 | `-w` 会直接重写文件，CI 悄悄改了代码却没人察觉，diff 也不再可信 | CI 用 `gofmt -l .` 只检查不改，有输出就打回；`-w` 留在本地编辑器保存时 |
| `go vet` 报出来的一定是真 bug | 它只报可疑，存在误报；也完全不覆盖"没写用例的那部分行为" | 先读懂再修，且要用 `go test` 和代码评审补上另一半 |
| 删掉一处 import 后 `go.mod` 会自动变干净 | 没有任何命令会在你保存文件时自动同步依赖清单 | 增删 import 之后显式跑 `go mod tidy` |
| `goimports` 是 `go` 命令内置的子命令 | 它需要单独安装，只是编辑器插件通常已内置 | 内置的是 `gofmt`；`goimports` 是它的超集 |
| `//go:generate` 这行会被编译进程序、自动执行 | 它只是注释，不写进二进制，也不在任何构建阶段自动跑 | 只有显式执行 `go generate ./...` 时，工具链才扫描并运行它 |

### 版本与平台边界

| 边界 | 具体差异 | 怎么处理 |
| --- | --- | --- |
| Go 1.19 | `gofmt` 开始重新排版文档注释，老项目升级后会出现大面积 diff | 先看清是排版变化还是逻辑变化，再一次性提交 |
| Go 1.21 | `go.mod` 支持 `toolchain` 指令 | 团队可据此统一工具链，老版本工具链不认该指令 |
| 随版本演进 | `go vet` 的检查项会新增，升级后可能冒出从没见过的告警 | 逐条读懂再修，不要直接忽略，也不要全局关闭 |
| 随版本演进 | `go tool dist list` 列出的平台组合会增减 | 构建脚本里查实时清单，别把组合写死 |
| `-race` 平台限制 | `go test -race` 需要特定架构与 CGO 支持，并非所有 GOOS/GOARCH 都能用 | 交叉编译时不要在脚本里无条件加，细节见 [第 049 讲](/concurrency/race) |
| Windows 与 Unix | `Makefile`、pre-commit 钩子、`head` 这类管道命令的写法不同 | 用 `go` 自带命令做核心检查，平台相关的包装放进脚本单独适配 |

<Quiz q="关于提交前的检查流水线，下面哪一项做法是正确的？" :options='["CI 里用 gofmt -w . 自动重写，保证风格永远正确","把 go vet 的告警全部忽略，只信 go test","CI 里用 gofmt -l . 检查排版，再依次跑 vet、test、build","go mod tidy 会在 go build 时自动执行，不必单独跑"]' answer="2" explain="gofmt -l 只列名字不改文件，适合放进 CI；vet 与 test 互补且都不能替代评审；tidy 需要显式执行，go build 不会替你同步 go.mod。" />

## 4. 工程实践

团队里真正省时间的做法，是把这条流水线固化下来而不是靠人记。用 Makefile 或脚本封装 `make check`（gofmt 检查 + vet + test），让本地和 CI 跑完全相同的命令；配一个 pre-commit 钩子自动 `gofmt -w`，杜绝"格式化提交"混进功能改动里，让 diff 干净可读。CI 里再加一条 `go build ./...` 作为最终闸门。

```bash
gofmt -w .
goimports -w .
go vet ./...
go test ./...
go mod tidy
go generate ./...
GOOS=linux GOARCH=amd64 go build -o app-linux .
go tool dist list
```

<CodeTabs :tabs='[{"label":"本地开发","code":"gofmt -w .\ngo test ./...","note":"快速反馈，改完立刻验证"},{"label":"提交前自查","code":"gofmt -l .\ngo vet ./...","note":"gofmt -l 有输出就说明还有文件没格式化"},{"label":"CI 发布","code":"go test ./...\ngo build -o app ./...","note":"固定 Go 版本，产物名用 -o 明确指定"}]' title="三个场景各跑哪几条" />

<Callout type="tip" title="一条实用经验" text="用 gofmt -l . 做检查而不是 gofmt -w .：前者只列出不规范的文件、不改动源码，适合放进 CI；后者直接重写文件，适合放在本地编辑器保存时。混用容易让 CI 悄悄改代码却没人发现。" />

交叉编译时 `go tool dist list` 会列出全部支持的平台组合，写构建脚本前查一眼比凭记忆可靠。生成的代码要不要提交进仓库，取决于项目约定：提交能让没有装生成器的同事直接构建，不提交则能保证生成物永远和源定义同步。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l0-guide/lesson-004-toolchain`](https://github.com/lfire/go/tree/main/examples/l0-guide/lesson-004-toolchain)。clone 仓库后进入 `examples/l0-guide`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="把紧凑代码改成 gofmt 规范" task="下面这行紧凑写法能编译，但排版不符合官方风格。请写出 gofmt 之后 main 函数应有的样子，用 Println 打印 total 的值。" :hints='["gofmt 会把函数体缩进一个制表符","把两条语句拆成两行，花括号各占一行"]' solution="func main() {\n    total := 40 + 2\n    fmt.Println(total)\n}" explain="gofmt 只调整排版，不改变语义，输出仍是 42。这也说明它做不了逻辑检查，逻辑要靠 vet 与 test。" />

## 随堂检测

<Quiz no="004-1" q="关于 gofmt 与 go vet，下面哪一项说法是正确的？" :options='["gofmt 提供丰富的缩进与换行配置项供团队自定义","go vet 报的问题一定会导致编译失败","gofmt 没有配置项，vet 抓的是编译能过但可疑的写法","goimports 是 go 命令内置的子命令"]' answer="2" explain="gofmt 刻意不提供配置项以统一全生态风格；vet 抓的是编译能过的可疑写法；goimports 需要单独安装，不是内置命令。" />

<Predict no="004-2" q="给工程删掉一处 import 之后直接提交，没跑 go mod tidy，最可能发生什么？" :options='["go.mod 里那条依赖会自动消失","go.mod 仍留着没人用的依赖，go.sum 也可能不同步","编译一定报错","go test 会自动替你清理"]' answer="1" explain="go mod tidy 才是负责按实际 import 增删依赖的命令；不跑它，声明与源码就会逐渐不一致。" />

<FillBlank no="004-3" tpl="统一格式用 go ___\n静态检查用 go ___\n清理依赖用 go mod ___" :answers='["fmt","vet","tidy"]' hint="三条都是 go 开头的高频命令，第三条是 go mod 的子命令。" explain="gofmt、go vet、go mod tidy 分别对应格式、静态检查、依赖清理，是提交前的三件套。" :explains='["go fmt 内部调用 gofmt 按固定规则重排缩进与对齐，让全体代码风格一致，提交前不必再手工争论空格。","vet 做静态分析，专抓 printf 格式串与参数不匹配这类编译能过、运行才出错的隐患，与编译互补。","tidy 依据源码真实 import 增删 require 并同步 go.sum，删掉不再用的依赖、补上间接依赖，须跟在 mod 之后。"]' />

## 一页纸速记

<KnowledgeCard :points='["gofmt 无配置项，负责统一排版；goimports 额外自动增删 import","go vet 抓编译能过但可疑的写法，go test 验证行为，二者互补","go mod tidy 按实际 import 同步 go.mod 与 go.sum，改依赖后必跑","go generate 执行源码里的生成指令；GOOS 与 GOARCH 控制交叉编译"]' :pitfalls='["别在 CI 里用 gofmt -w 悄悄改代码，改用 gofmt -l 做检查","别忽略 vet 告警，升级 Go 后新增告警要先读懂再修","别把 go generate 的产物和源定义搞成两份不同步的手工副本"]' summary="把格式、检查、测试、依赖、生成、构建固化成一条流水线，质量就不再依赖个人记性。" />

## 小结与下一讲

- 工具链统一是 Go 的核心体验：`gofmt` 管外观、`vet` 管可疑写法、`test` 管行为、`mod tidy` 管依赖。
- 提交前的推荐顺序是格式化检查、静态检查、单测、再编译，并把它固化进 Makefile 与 CI。
- `go generate` 消灭重复代码，`GOOS`/`GOARCH` 让交叉编译一行命令搞定。

工具链已经齐备，从下一讲起进入编程地基，先回答一个最根本的问题：程序到底在处理什么。

[下一讲：程序、数据、值与类型](/foundation/concepts)
