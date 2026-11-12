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

### 为什么 Modules 是一次"不得不"的演进

只记住"代码可以放任意目录"会漏掉重点。GOPATH 模型能推出三个具体的、必然发生的故障：

第一，全机只有一个 `$GOPATH/src`，所有工程的源码与第三方依赖都堆在同一棵树里，依赖没有版本概念，实质是"全机共享一份最新的代码"。第二，于是 A 工程要 v1、B 工程要 v2 无法同时满足；某个人 `go get -u` 升了一个库，所有工程一起受影响；换一台机器也无法重建出相同的依赖集合，因为没有任何文件记录"当时用的是哪一版"。第三，前两条合起来，构建结果就不可复现——这正是本讲反复强调 `go.sum` 必须提交的根本原因。

Go Modules 用"每个工程自带一份 `go.mod`（声明要哪些模块、各自什么版本）加一份 `go.sum`（记录每份内容的校验和）"，同时回答了**隔离**与**复现**两个问题。所以"代码可以放在任意目录"只是顺带的结果，真正被解决的是依赖的隔离与可复现。回到厨房类比：这相当于从"全楼共用一间储藏室"改成"每道菜自带一张配料清单和防伪码"，谁也不会再动到别人的原料。

### 版本管理为什么值得单独占一条

教程以 Go 1.21 及以上为基线，但线上老项目可能锁在 1.19；`golang.org/dl` 让多个工具链版本互不干扰地共存。为什么不用系统包管理器直接覆盖升级？因为 Go 的兼容承诺保证的是"旧代码在新版能编译"，但新版同时会改动 `gofmt` 对文档注释的排版（1.19 起）、`go vet` 的检查项、以及循环变量的语义（1.22 起）。覆盖式升级会让"昨天还通过的构建"今天行为悄悄变化，却找不到是哪行代码引起的。

多版本共存于是和 Modules 锁依赖是同一个思路：让每个工程明确声明自己认哪一版，而不是全机共用一个"最新"。这也解释了为什么 CI 里要固定版本、不要写 `latest`（[第 04 讲](/guide/toolchain) 与 [第 60 讲](/engineering/cicd) 会再提）。

### 三个立刻能跑的体检命令

```bash
go mod init example.com/try && cat go.mod
```

<OutputBox title="实验一：看一眼刚生成的 go.mod（版本随工具链而变）" output="module example.com/try\n\ngo 1.23" />

只有两行：`module` 是这个工程对外被 import 时的名字，`go` 是它声明的语言版本，没有 `require` 说明此刻零依赖。命令能成功本身就验证了"任意目录即可初始化"，不必先建 GOPATH 目录结构。

```bash
go env GOPATH GOMODCACHE GOPROXY
```

<OutputBox title="实验二：看清三个路径各自管什么" output="/Users/you/go\n/Users/you/go/pkg/mod\nhttps://proxy.golang.org,direct" />

`GOPATH` 在 Modules 时代主要只剩"缓存的父目录"这一个作用，不再限制代码位置；`GOMODCACHE` 是下载下来的模块真正存放的地方，位于 GOPATH 下的 `pkg/mod`；`GOPROXY` 用逗号分隔多个地址，末尾的 `direct` 表示前面都取不到时直接连源站。三个值一起看，就能把"代码放哪、依赖存哪、依赖从哪来"分得清清楚楚。

```bash
go env -w GOPROXY=https://goproxy.cn,direct && go env GOPROXY
```

<OutputBox title="实验三：改完立刻复核" output="https://goproxy.cn,direct" />

`-w` 把设置写进用户级配置文件，而不是当前终端，所以下次开终端仍然生效；末尾再跑一次 `go env GOPROXY` 复核，是环境配置里最该养成的"改完立刻确认"习惯。注意 `os.Getenv("GOPROXY")` 读的是进程环境变量，与 `go env` 显示的默认值不是一回事，本讲第二段的 if 分支正是为了区分这两者。

```bash
go doc runtime.Version
```

<OutputBox title="实验四：文档来自本机源码（节选一行）" output="func Version() string" />

`go doc` 不联网、直接读本机标准库源码。顺带澄清一个易混点：`go version` 查的是**工具链**版本，`runtime.Version()` 查的是**正在运行这个程序的运行时**版本，平时两者一致，但交叉编译或远程执行时语义并不相同。

### 这些知识点会在后面哪些讲反复出现

| 本讲埋下的点 | 后面在哪用到 |
| --- | --- |
| `go.mod` / `go.sum` | [第 04 讲](/guide/toolchain) 的 tidy、[第 11 讲](/foundation/packages) 的语义化版本与 replace、[第 90 讲](/distributed/docker) 的镜像构建 |
| GOPROXY / GOPRIVATE / GOSUMDB | [第 60 讲](/engineering/cicd) 的 CI 拉取依赖与供应链校验 |
| GOOS / GOARCH | [第 03 讲](/guide/hello) 与 [第 04 讲](/guide/toolchain) 的交叉编译 |
| `runtime.Version` / `NumCPU` | [第 39 讲](/concurrency/goroutine) 的 GOMAXPROCS |
| Delve 断点 | [第 59 讲](/engineering/profiling) 与阶段四排查并发问题的通用手段 |

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

### 推导：go.sum 为什么必须跟着源码提交

原文给了"go.sum 必须提交"这个结论，把它拆开看是一条校验链：`go.mod` 里记的是"我要哪个模块、哪一版"；真正下载到本机的是一个压缩包，其内容可能被镜像、网络中间环节或上游误发所改变；`go.sum` 里存的是那份内容对应的密码学校验和。于是每次构建，工具链都会把新下载内容算出的校验和与 `go.sum` 里已记录的那一份比对，不一致就拒绝构建。

由此能推出三件事。第一，`go.sum` 不是缓存，而是"我当初认可的依赖长什么样"这份基线；删掉它，工具链只能重新生成一份"记录现在拿到的是什么"，却无法证明它和当初是同一份。第二，CI 里跑 `go mod verify` 复核的是同一个性质。第三，这也解释了为什么 `GOSUMDB` 要和 `GOPROXY` 配套——代理负责"从哪拿"，校验和数据库负责"拿到的和官方公布的一致吗"，两者缺一，可复现构建就少了一环。

### 零基础最容易照抄错的五个理解

| 错误理解或写法 | 为什么站不住 | 正确理解 |
| --- | --- | --- |
| 要手动 `export GO111MODULE=on` 才算用上 Modules | 该开关在 Go 1.16 起默认就是 on，早已成为默认模式 | 看到教你设它、或把代码塞进 `GOPATH/src` 的资料，基本可判定是旧版教程 |
| 模块路径必须是一个能真实访问的网址 | `go mod init` 只把它当"这个工程被 import 时的名字"，本地练习不联网也照常用 | 本地写 `example.com/try` 即可；只有真要发布、被别人拉取时才需要对应真实仓库 |
| `go.sum` 是编辑器生成的缓存，删掉会自动重建，不必提交 | 重建出来的只是"当前拿到的内容"的记录，失去了与原来那份的比对基线 | 与 `go.mod` 一起进版本库 |
| 配好公共 `GOPROXY` 就能拉私有仓库 | 公共代理根本不知道你的私有仓库，且可能把模块路径发到公共校验库 | 用 `GOPRIVATE` 或 `GONOPROXY`、`GONOSUMDB` 把私有路径排除在外 |
| `go env -w` 只对当前终端生效 | `-w` 写的是用户级配置文件，跨终端、跨重启都在 | 想临时覆盖只对一条命令生效，应写成命令前缀，如 `GOOS=linux go build` |

### 版本与平台边界

| 边界 | 具体差异 | 怎么处理 |
| --- | --- | --- |
| Go 1.16 | Modules 成为默认，`GO111MODULE` 不再是必配项 | 不再照老教程设 GOPATH 工作区 |
| Go 1.18 | 加入 `go.work`，支持多模块联合开发 | 单模块练习用不到，知道它存在即可 |
| Go 1.21 | `go.mod` 支持 `toolchain` 指令声明推荐工具链 | 团队统一约定时可写，老版本工具链不认 |
| 安装路径 | macOS 常见 `/opt/homebrew/bin` 或 `/usr/local/go`，Linux 常在 `/usr/local/go`，Windows 默认在用户目录下 | 用 `go env GOROOT` 查实际位置，别照抄别人的路径 |
| GOPATH 默认值 | macOS 与 Linux 是 `~/go`，Windows 是 `%USERPROFILE%` 下的 `go` | 用 `go env GOPATH` 读，不要在脚本里硬编码 |
| 环境变量写法 | `export`、`setx`、PowerShell 语法各不相同 | 统一用 `go env -w`，它是跨平台一致的那条路 |
| 路径分隔符 | Windows 用反斜杠，另两者用正斜杠 | Go 源码里用正斜杠写 import 路径，工具链会处理 |

最后一行尤其值得记：import 路径是 Go 层面的逻辑路径，不随操作系统的文件系统分隔符改变，这也是"同一份源码能跨平台构建"的一个小前提。

<Predict q="把 go.sum 删掉后把工程交给同事，同事直接执行 go build，最可能发生什么？" :options='["照常构建，和原来完全一样","报缺少 go.sum 条目，要求先 go mod download","自动从 go.mod 里补出完整校验和","提示必须重新执行 go mod init"]' answer="1" output="missing go.sum entry for module providing package; to add: go mod download" explain="校验和基线没了，工具链无法确认下载到的内容与当初认可的那份一致，于是拒绝继续。重新生成只是记录现在拿到了什么，并不能证明它没被换过，所以 go.sum 必须随源码一起提交。" />

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

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l0-guide/lesson-002-setup`](https://github.com/lfire/go/tree/main/examples/l0-guide/lesson-002-setup)。clone 仓库后进入 `examples/l0-guide`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="写个环境自检小程序" task="写一个程序，同时打印当前 Go 版本和机器的逻辑 CPU 数量，用来确认工具链装好、且 runtime 包可用。" :hints='["两个值都在 runtime 包里，版本是 Version()，CPU 数是 NumCPU()","只需要 fmt 和 runtime 两个 import"]' solution="func main() {\n    fmt.Println(runtime.Version())\n    fmt.Println(runtime.NumCPU())\n}" explain="Version 返回版本字符串，NumCPU 返回可用的逻辑核心数。输出结果随机器不同而变，能让两个值都打印出来就说明环境通了。" />

## 随堂检测

<Quiz no="2-1" q="关于 Go Modules，下面哪一项是正确的？" :options='["代码必须放在 GOPATH/src 目录下才能编译","每个工程的依赖版本写在 go.mod 与 go.sum 里，代码可以放在任意目录","go.sum 是编辑器生成的缓存，应当删掉不提交","模块路径必须是一个能真实访问的网址"]' answer="1" explain="Modules 让代码脱离 GOPATH 限制，go.mod 声明依赖，go.sum 记录校验和保证可复现构建，必须一起提交。" />

<Predict no="2-2" q="环境变量 GOPROXY 从未被设置过时，第二段代码会打印什么？" :options='["打印一串默认代理地址","打印 未显式设置，使用默认值","编译报错","什么也不打印"]' answer="1" explain="os.Getenv 读不到变量时返回空字符串，代码里的 if 分支正是用来处理这种情况；注意这和 go env 显示的默认值是两回事。" />

<FillBlank no="2-3" tpl="用命令 go mod ___ 初始化一个模块\npackage ___ 声明这个包会被编译成可运行程序" :answers='["init","main"]' hint="第一空是 go mod 后面的子命令，第二空和第 01 讲填的是同一个词。" explain="go mod init 在当前目录生成 go.mod；package main 则是可执行程序的固定包名，两个知识点在这里接上了。" :explains='["模块初始化要用 go mod 的子命令，它会生成 go.mod 写入模块路径与 Go 版本，后续依赖都挂在这份文件上。","可执行程序必须用固定的入口包名，编译器据此链接成二进制并约定从同名的入口函数开始执行，换成其它包名只会得到库文件。"]' />

## 一页纸速记

<KnowledgeCard :points='["工具链开箱即用：go version 验证，go mod init 在任意目录初始化模块","go.mod 声明模块路径与依赖，go.sum 记录校验和，两者都要提交","GOPROXY 决定依赖从哪下载；go env 是排查环境问题的总开关","调试用 IDE 插件加 Delve 断点，别靠打印语句排查"]' :pitfalls='["别照老教程设 GOPATH/src 或 GO111MODULE，1.16 起 Modules 已是默认","别删 go.sum，也别在 CI 里用不固定版本的 latest","私有模块要配 GOPRIVATE，否则可能被发到公共代理"]' summary="环境的目标不是能跑一次，而是任何一台机器上都能复现同一份构建。" />

## 小结与下一讲

- 安装后先用 `go version` 与一段运行时代码双重验证，需要多版本时用 `golang.org/dl` 管理。
- Go Modules 让代码脱离 GOPATH，`go.mod` 与 `go.sum` 共同保证依赖可复现，二者都要进版本库。
- 代理、校验库、私有仓库变量和调试器是团队协作的基础设施，第一天就配对比后面补更省事。

环境已经就绪，接下来把第一个程序的每一行都看明白。

[下一讲：第一行 Go 代码：从源码到可执行文件](/guide/hello)
