---
title: 第 02 讲 · 装好环境：工具链、工作区与 Modules
description: 在本机跑通安装、模块初始化与 IDE 调试，把环境问题一次解决掉
---

<StageHeader stage="L0 · 导学" level="★☆☆☆☆" time="25 分钟" goal="在本机跑通安装、模块初始化与 IDE 调试" prereq="第 01 讲" />

# 第 02 讲 · 装好环境：工具链、工作区与 Modules

> 环境问题是学习路上最不划算的中断：它跟语法无关，却会在后面每一讲反复冒出来。很多人卡在"代码明明照抄却跑不起来"，最后发现只是模块没初始化、代理没配、或者照着一篇 2018 年的老教程去设 GOPATH。这一讲把安装、工作区、环境变量和调试器一次配齐，并且专门标注哪些做法已经过时。

## 这一讲你会得到什么

<CheckList id="L0-02" title="本讲目标" :items='["能在自己的系统上安装并验证 Go 工具链，需要时管理多个版本","能说清 GOPATH 时代到 Go Modules 的演进，并独立初始化一个模块","能看懂并配置 go env 里的关键变量，尤其是 GOPROXY","能用 VS Code 或 GoLand 配合 Delve 打第一个断点"]' />

## 1. 先建立直觉

把开发环境想成一间厨房：工具链是灶具（编译器、链接器、标准库），工作区是案板（你的代码放在哪），Modules 是菜谱上的配料清单（这个工程依赖哪些外部代码、各自是什么版本），而代理是送货渠道（从哪里把配料取回来）。早年 Go 要求所有案板必须摆在同一个固定房间（GOPATH），配料也没有版本概念，几个工程互相污染是家常便饭。Go Modules 把这两件事都解决了：代码可以放在任意目录，依赖版本写进一份明确的清单。

<ConceptDiagram type="hello-flow" title="装好环境后，一次 go run 的完整链路" />

注意图上这条链路：从源码到可执行文件再到进程，中间所有环节都由工具链自动完成。你不需要手工调用编译器，也不需要像解释型语言那样在目标机装运行时——这正是第 01 讲说的"编译型"，现在它变成了你机器上一条可以亲手敲的命令。

<FlowTimeline :steps='[{"title":"安装工具链","desc":"安装包或包管理器，go version 验证","tone":"blue"},{"title":"初始化模块","desc":"任意目录执行 go mod init","tone":"green"},{"title":"配置环境","desc":"go env 查看，按需设置 GOPROXY","tone":"orange"},{"title":"接上调试器","desc":"装好 IDE 插件与 Delve，打断点","tone":"purple"}]' direction="vertical" />

## 2. 语法与规则

先安装并验证。不同系统的入口略有差别，下面三种方式任选其一：

<CodeTabs :tabs='[{"label":"macOS","code":"brew install go\ngo version","note":"Homebrew 安装的路径通常在 /opt/homebrew/bin 或 /usr/local/go"},{"label":"Ubuntu","code":"sudo apt install golang-go\ngo version","note":"发行版自带版本可能偏旧，需要新特性时改用官方包"},{"label":"多版本管理","code":"go install golang.org/dl/go1.23.8@latest\ngo1.23.8 download","note":"用 go 命令本身安装指定版本，适合同时维护多个项目"}]' title="按系统选择安装方式" />

装好后用程序确认运行时版本与平台，比只看一条命令更有说服力：

```go
package main

import (
	"fmt"
	"runtime"
)

func main() {
	fmt.Println("Go 版本：", runtime.Version())            // 运行时版本，对应 go version
	fmt.Println("系统 / 架构：", runtime.GOOS, runtime.GOARCH) // 交叉编译的两个关键变量
}
```

<OutputBox title="运行结果（版本号随你的安装而变）" output="Go 版本： go1.23.8\n系统 / 架构： darwin arm64" guess="true" />

<LineByLine code="v := runtime.Version()\ngoos := runtime.GOOS\nfmt.Println(v, goos)" :notes='["runtime.Version 返回当前运行时的版本字符串","GOOS 和 GOARCH 是编译期常量，标明目标系统与 CPU 架构","把两者一起打印，用来核对交叉编译有没有设对"]' />

接着初始化工作区。这两行命令是本讲最该记牢的操作：

```bash
mkdir hello-go && cd hello-go
go mod init example.com/hello-go
```

`go mod init` 会在当前目录生成一份 `go.mod`，里面写着模块路径和 Go 版本。模块路径只是这个工程对外被引用的名字，本地练习用任意合法名字都行，不必对应真实网址。关键是：目录在哪都可以，再也不用塞进 GOPATH。

第二段代码读取环境变量，验证代理配置有没有生效：

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	proxy := os.Getenv("GOPROXY") // 读取进程环境变量
	if proxy == "" {
		proxy = "未显式设置，使用默认值"
	}
	fmt.Println("当前 GOPROXY：", proxy)
}
```

第三段代码确认"工作区可以是任意目录"这件事：

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	dir, err := os.Getwd() // 取当前工作目录
	if err != nil {
		fmt.Println("取工作目录失败：", err)
		return
	}
	fmt.Println("当前目录名：", filepath.Base(dir)) // 只取最后一段
}
```

<OutputBox title="运行结果（取决于你把工程放在哪）" output="当前目录名： hello-go" />

## 3. 深入一步

<Term word="模块路径" text="go.mod 第一行的 module 指令，是这个工程被外部 import 时的名字，也是依赖解析的起点。" />

<Term word="GOPROXY" text="模块下载代理，用逗号分隔多个地址，direct 表示最后直接连源站。" />

`go env` 是排查环境问题的总开关，常用变量如下：

| 变量 | 作用 | 典型值或说明 |
| --- | --- | --- |
| GOROOT | Go 工具链与标准库的安装位置 | 由安装包决定，一般不用手动改 |
| GOPATH | 默认工作区与模块缓存的父目录 | Modules 时代已不再限制代码位置 |
| GOMODCACHE | 下载下来的模块缓存目录 | 位于 GOPATH 下的 pkg/mod |
| GOPROXY | 模块下载代理 | 国内常配成可访问的镜像加 direct |
| GOSUMDB | 校验模块完整性的校验和数据库 | 与代理配套，保证依赖没被篡改 |
| GOOS / GOARCH | 目标系统与 CPU 架构 | 交叉编译时用环境变量临时覆盖 |

<Callout type="warn" title="最容易照抄错的老配置" text="GO111MODULE 在 Go 1.16 起默认就是 on，Go Modules 早已成为默认模式。凡是把代码强行放进 GOPATH/src、或者教你 export GO111MODULE=auto 的教程都是过时的。另外 go.mod 里从 Go 1.21 起可以写 toolchain 指令，用来声明推荐的工具链版本，老版本工具链会忽略或不认，团队协作时值得统一约定。" />

再看一眼演进本身，能帮你判断某段资料是不是过时了：

<Compare :panels='[{"label":"GOPATH 时代","code":"代码必须放在 $GOPATH/src 下\n依赖没有版本，全靠本地一份副本","note":"多个工程共享同一份依赖，升级一个就影响全部"},{"label":"Go Modules","code":"任意目录执行 go mod init\ngo.mod 与 go.sum 锁定依赖版本","note":"每个工程独立声明依赖，可复现构建"}]' title="同一个工程，两种工作区模型" />

## 4. 工程实践

真实团队里，环境一致性比"能跑"更重要。第一，把 `go.mod` 和 `go.sum` 一起提交进版本库：`go.sum` 记录每个依赖的校验和，删掉它等于放弃了可复现构建与供应链校验。第二，在 CI 里固定 Go 版本，别让 `latest` 悄悄把构建行为改掉；需要统一时可以用 `go` 指令加 `toolchain` 指令声明。第三，公司私有仓库要配 `GOPRIVATE` 或 `GONOPROXY`，避免私有模块被发到公共代理或校验库。第四，`vendor` 目录只在离线构建、严格审计等场景才需要，日常别提交一堆冗余代码。

调试方面，VS Code 安装官方 Go 扩展（底层是 gopls 语言服务器）即可获得补全、跳转与内联提示；命令行调试器 Delve 用下面两条命令安装和启动：

```bash
go install github.com/go-delve/delve/cmd/dlv@latest
dlv debug ./...
```

<Callout type="tip" title="调试习惯" text="从第一天起就用断点观察变量，而不是到处插 fmt.Println。断点能看调用栈与局部变量，排查完不用回头删代码，这是后面读并发、读运行时源码时最省时间的习惯。" />

配好代理用一条命令即可，改完立刻用 `go env` 复核：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
go env GOPROXY GOMODCACHE GOROOT
```

## 动手练一练

<Exercise title="写个环境自检小程序" task="写一个程序，同时打印当前 Go 版本和机器的逻辑 CPU 数量，用来确认工具链装好、且 runtime 包可用。" :hints='["两个值都在 runtime 包里，版本是 Version()，CPU 数是 NumCPU()","只需要 fmt 和 runtime 两个 import"]' solution="func main() {\n    fmt.Println(runtime.Version())\n    fmt.Println(runtime.NumCPU())\n}" explain="Version 返回版本字符串，NumCPU 返回可用的逻辑核心数。输出结果随机器不同而变，能让两个值都打印出来就说明环境通了。" />

## 随堂检测

<Quiz no="02-1" q="关于 Go Modules，下面哪一项是正确的？" :options='["代码必须放在 GOPATH/src 目录下才能编译","每个工程的依赖版本写在 go.mod 与 go.sum 里，代码可以放在任意目录","go.sum 是编辑器生成的缓存，应当删掉不提交","模块路径必须是一个能真实访问的网址"]' answer="1" explain="Modules 让代码脱离 GOPATH 限制，go.mod 声明依赖，go.sum 记录校验和保证可复现构建，必须一起提交。" />

<Predict no="02-2" q="环境变量 GOPROXY 从未被设置过时，第二段代码会打印什么？" :options='["打印一串默认代理地址","打印 未显式设置，使用默认值","编译报错","什么也不打印"]' answer="1" explain="os.Getenv 读不到变量时返回空字符串，代码里的 if 分支正是用来处理这种情况；注意这和 go env 显示的默认值是两回事。" />

<FillBlank no="02-3" tpl="用命令 go mod ___ 初始化一个模块\npackage ___ 声明这个包会被编译成可运行程序" :answers='["init","main"]' hint="第一空是 go mod 后面的子命令，第二空和第 01 讲填的是同一个词。" explain="go mod init 在当前目录生成 go.mod；package main 则是可执行程序的固定包名，两个知识点在这里接上了。" />

## 一页纸速记

<KnowledgeCard :points='["工具链开箱即用：go version 验证，go mod init 在任意目录初始化模块","go.mod 声明模块路径与依赖，go.sum 记录校验和，两者都要提交","GOPROXY 决定依赖从哪下载；go env 是排查环境问题的总开关","调试用 IDE 插件加 Delve 断点，别靠打印语句排查"]' :pitfalls='["别照老教程设 GOPATH/src 或 GO111MODULE，1.16 起 Modules 已是默认","别删 go.sum，也别在 CI 里用不固定版本的 latest","私有模块要配 GOPRIVATE，否则可能被发到公共代理"]' summary="环境的目标不是能跑一次，而是任何一台机器上都能复现同一份构建。" />

## 小结与下一讲

- 安装后先用 `go version` 与一段运行时代码双重验证，需要多版本时用 `golang.org/dl` 管理。
- Go Modules 让代码脱离 GOPATH，`go.mod` 与 `go.sum` 共同保证依赖可复现，二者都要进版本库。
- 代理、校验库、私有仓库变量和调试器是团队协作的基础设施，第一天就配对比后面补更省事。

环境已经就绪，接下来把第一个程序的每一行都看明白。

[下一讲：第一行 Go 代码：从源码到可执行文件](/guide/hello)
