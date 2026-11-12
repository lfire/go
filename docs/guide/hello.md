---
title: 第 03 讲 · 第一行 Go 代码：从源码到可执行文件
description: 把最小程序的每一行都讲透，并说清 go run 与 go build 到底差在哪
---

<StageHeader stage="L0 · 导学" level="★☆☆☆☆" time="25 分钟" goal="彻底看懂一个最小 Go 程序的每一行" prereq="第 02 讲" />

# 第 03 讲 · 第一行 Go 代码：从源码到可执行文件

> 大多数人的第一行 Go 代码是照抄来的：复制、粘贴、运行、看到一行字，然后就翻篇了。问题在于，这四行里藏着 Go 最核心的几个约定——包怎么组织、入口怎么定、名字大小写意味着什么、编译器对"没用到的东西"有多严格。这一讲把最小程序逐行拆开，顺便讲清 `go run` 和 `go build` 的区别。养成"每行都问为什么"的习惯，后面一百多讲会轻松得多。

## 这一讲你会得到什么

<CheckList id="L0-03" title="本讲目标" :items='["能逐行解释 package main、import、func main、fmt.Println 各自的作用","能说清未使用的导入为什么会直接导致编译失败","能区分 Print、Println、Printf 三种打印方式","能判断什么场景用 go run、什么场景用 go build"]' />

## 1. 先建立直觉

把源码想成菜谱，编译器是想成厨师，可执行文件是做好的菜。`go run` 相当于"现做现吃"：厨师在后台临时找个灶台把菜做出来，端给你吃完，灶台随即清理，不留下成品。`go build` 相当于"打包带走"：菜做好后装进盒子放在你面前，你想什么时候吃、拿到哪台机器上吃都行。两者用的是同一位厨师、同一份菜谱，差别只在产物留不留、留在哪。

<ConceptDiagram type="hello-flow" title="从 main.go 到屏幕上的一行字" />

<ConceptDiagram type="compile-link" title="编译器在背后做完的四件事" />

第二张图值得多看一眼：源码先被解析成语法树、做类型检查、优化并生成目标平台的机器码，最后把标准库一起链接进来。因此 `go run` 绝不是"解释执行"，你也从来不需要一个"Go 解释器"。

### 为什么这四行长得这么"刻板"

初学最容易把 `package main`、`import`、`func main` 当成一套要死记的仪式。其实每一条都能从"工具链要零配置地把源码变成可执行文件"倒推出来。

`package main` 为什么必须是 `main`：链接器需要一个确定的入口符号，Go 用"包名加函数名"的约定代替一份配置文件，于是工具链不用读任何配置就知道这个文件要编成可执行程序还是库。替代方案是像某些语言那样在构建脚本里声明入口，代价是每个工程多一份约定、工具链多一步解析。

`import` 为什么写在文件顶部且必须精确：编译器要在**不执行代码**的前提下构造依赖图；放在顶部让"这个文件依赖什么"一眼可见。未使用的 import 直接报错，正是这条规则的强制副作用——依赖图里不允许出现一条没用到的边。

`func main` 为什么无参数无返回值：入口由运行时调用，命令行参数从 `os.Args` 读取，成败由进程退出码表达（[第 12 讲](/foundation/capstone) 会用非零退出码）。把签名定死，工具链就不必处理"入口千奇百怪"的情况。

首字母大写决定导出，则是用一条命名规则替代了 `public`、`private` 这类关键字。代价是命名不再只是好看，它直接承担了可见性语义。

把这四条连起来，"菜谱与厨师"的类比还能再推一层：`go run` 与 `go build` 用的是同一位厨师、同一份菜谱、同一套编译流程，所以产物的质量完全一样，差别只在成品留在哪、留多久——`go run` 绝不是"简化版编译"或"解释执行"。

### 三个能立刻跑的小实验

```bash
go build -o hello main.go && ls -l hello && ./hello
```

<OutputBox title="实验一：产物留下、可反复运行" output="-rwxr-xr-x  1 you  staff  1234567 hello\nHello, Go!" />

`ls -l` 那行说明产物是一个有可执行权限的二进制文件（数字与权限位随平台而变），而不是文本；紧接着直接运行它，打印出和 `go run` 完全一样的一行。把 `-o` 指向别的目录、甚至把 `main.go` 临时移走再执行，`./hello` 依旧能跑，这就是"二进制自带所需代码"的直接证据。Windows 上没有 `ls`，产物名会是 `hello.exe`，对应命令换成 `dir hello.exe`。

```bash
time go run main.go
time ./hello
```

<OutputBox title="实验二：两条命令的耗时结构（示意）" output="go run：real 0m0.35s，其中含一次完整编译\n./hello：real 0m0.01s，只启动已编好的二进制" />

同一份源码、同一个输出，耗时差一个数量级，原因就是原文提到的那点：`go run` 每次都要重走一遍编译流程，只是把结果放进临时目录；`./hello` 只启动现成的机器码。这个实验也解释了为什么压测与基准测试必须先 `go build`，不能拿 `go run` 的耗时当性能数据。

```bash
go build
```

<OutputBox title="实验三：留一个没用到的 import 会怎样（示意）" output="./main.go:4:8: math imported and not used" />

把第三段代码里用到 `math.Pi` 的那行删掉、却保留 `import math`，再执行 `go build`，编译器会在生成机器码之前就拒绝，并精确指出是哪个文件的哪一行。逐项读这条报错：`./main.go` 是文件，`4:8` 是行列，`imported and not used` 是原因。亲手跑一次，比记住"Go 很严格"这句结论牢得多。

### 这些约定会在后面哪些讲反复出现

| 本讲埋下的点 | 后面在哪用到 |
| --- | --- |
| `package` 与 `import` 的组织方式 | [第 11 讲](/foundation/packages) 的包、模块与 internal 可见性 |
| 首字母大小写决定导出 | [第 18 讲](/basics/methods) 的方法集、[第 19 讲](/basics/interfaces) 的接口实现 |
| `go run` 与 `go build` 的分工 | [第 04 讲](/guide/toolchain) 的命令地图、[第 60 讲](/engineering/cicd) 的 CI 构建闸门 |
| GOOS / GOARCH 交叉编译 | [第 90 讲](/distributed/docker) 的多阶段镜像 |
| 格式串与 `fmt` 家族 | [第 07 讲](/foundation/numeric) 的 `%T`、[第 08 讲](/foundation/strings) 的 `%c` |

## 2. 语法与规则

先看这个最小程序，它只有四行有效代码：

```go
package main // 声明这是可独立运行的程序包

import "fmt" // 导入标准库中负责格式化输出的包

func main() { // 程序入口，函数名固定、无参数、无返回值
	fmt.Println("Hello, Go!") // 打印一行文本并自动换行
}
```

<OutputBox title="运行结果" output="Hello, Go!" guess="true" />

逐行看：`package main` 说明这个包要被编译成可执行文件而不是库；`import` 声明本文件用到的外部包，路径就是包在标准库或模块里的位置；`func main` 是操作系统真正调用的第一个函数，名字固定且不能带参数与返回值；最后一行调用 `fmt` 包里的 `Println`。Go 不写分号，靠花括号划分代码块，缩进由 `gofmt` 统一，不参与语义。

<LineByLine code="msg := 42\nfmt.Println(msg)\nfmt.Println(msg + 1)" :notes='["短声明一个变量并赋值为 42，类型由编译器推断为 int","Println 打印变量的值，并在结尾自动补一个换行","Println 也能直接打印表达式，先算出 43 再输出，各占一行"]' />

注意 `fmt.Println` 首字母大写。这在 Go 里不是风格偏好，而是规则：首字母大写的标识符是<Term word="导出标识符" text="首字母大写的包级标识符可被其他包访问；小写则只在包内可见。" />，能被别的包使用；小写则只在包内可见。包名本身习惯全小写，所以你看到的是 `fmt.Println` 而不是 `Fmt.Println`。

`fmt` 家族有三个常用函数，差别只在换行与格式化：

<CodeTabs :tabs='[{"label":"Println","code":"fmt.Println(a, b)","note":"参数之间自动加空格，结尾自动换行，最常用"},{"label":"Printf","code":"fmt.Printf(format, a)","note":"用格式串里的格式符控制输出，不自动换行"},{"label":"Print","code":"fmt.Print(a, b)","note":"原样打印，不自动加空格也不自动换行"}]' title="fmt 包三种打印方式" />

把它们放在一起跑一遍，输出就清楚了：

```go
package main

import "fmt"

func main() {
	name := "Gopher"
	fmt.Println(name)                   // Println：打印后自动换行
	fmt.Printf("长度是 %d\n", len(name)) // Printf：按格式符格式化，换行要自己写
	fmt.Print("A", "B", "\n")           // Print：不自动换行，手动补一个
}
```

<OutputBox title="运行结果" output="Gopher\n长度是 6\nAB" guess="true" />

第三段代码演示 import 的分组写法，以及"导出"规则的实际用法：

```go
package main

import (
	"fmt"
	"math"
)

func main() {
	// math.Pi 首字母大写，说明它是可供外部使用的导出常量
	fmt.Printf("圆周率约等于 %.2f\n", math.Pi)
}
```

<OutputBox title="运行结果" output="圆周率约等于 3.14" />

多个导入用括号写成一组，是 Go 的标准写法。`%.2f` 表示按浮点数保留两位小数，所以 3.14159 被截成 3.14。

## 3. 深入一步

<Term word="包" text="Go 的最小编译与组织单元，一个目录下的所有 .go 文件属于同一个包。" />

<Term word="可执行文件" text="编译链接后的独立产物，内含运行所需的机器码与标准库代码，无需外部运行时。" />

| 维度 | go run main.go | go build |
| --- | --- | --- |
| 产物位置 | 系统临时目录 | 当前目录，或用 -o 指定 |
| 是否保留 | 运行完即清理，不留文件 | 留下可执行文件 |
| 重复执行 | 每次都重新编译一遍 | 编译一次，之后直接运行二进制 |
| 典型场景 | 本地快速迭代、试一段小程序 | 发布、CI 构建、打容器镜像 |
| 能否分发 | 否，产物是临时的 | 是，拷到目标机即可运行 |

<Callout type="warn" title="Go 编译器比你想的严格" text="未使用的 import 和未使用的局部变量都是编译错误，不是警告。上面第三段代码如果删掉 math.Pi 那行却留着 import math，会直接报 imported and not used。这是刻意设计：强制代码里不残留无用依赖，让大型仓库的依赖关系始终干净。另外 main 函数不能有参数和返回值，写错同样是编译失败。" />

<FlowTimeline :steps='[{"title":"解析源码","desc":"词法与语法分析，构建语法树","tone":"blue"},{"title":"类型检查","desc":"核对类型与未使用的导入","tone":"purple"},{"title":"生成机器码","desc":"按 GOOS 与 GOARCH 产出目标指令","tone":"orange"},{"title":"链接","desc":"合并标准库，得到可执行文件","tone":"green"}]' direction="horizontal" />

<Compare :panels='[{"label":"开发时","code":"go run main.go","note":"改一行就重跑一次，不关心产物落在哪"},{"label":"交付时","code":"go build -o app main.go && ./app","note":"产物明确、可复现、能拷到别的机器"}]' title="同一份源码，两种使用方式" />

### 推导：未使用的导入为什么必须是错误，而不是警告

原文给了"Go 编译器很严格"这个结论，顺着编译流程往下推，就能明白这份严格不是洁癖：编译器先按 `import` 构造依赖图，再以包为单位编译并把结果放进构建缓存；一个包被列为依赖，就意味着它会参与依赖解析、参与缓存键的计算。如果允许"导入了却没用"的边存在，依赖关系会逐次改动慢慢失真，改动一个无关包时也可能触发多余的重编，最终又回到第 01 讲说的"改一处、编一片"。

所以把未使用导入判成错误，实际是在强制维护一条不变量：**源码里出现的每条依赖边都是真实需要的**。同样的逻辑也作用于未使用的局部变量——它往往意味着一段写了一半或已经废弃的逻辑，让编译器拦下比留到评审时靠人眼发现便宜得多。这也是为什么这条规则不需要配置项：它保证的是依赖图的正确性，而不是排版偏好（排版才归 `gofmt` 管）。

### 零基础最容易带进来的五个误解

| 错误理解 | 为什么站不住 | 正确理解 |
| --- | --- | --- |
| `go run` 是边读边解释执行 | 它每次都完整走一遍解析、类型检查、生成机器码、链接，只是把产物放进临时目录 | `go run` 是"编译后立即执行"，和 `go build` 用同一套流程 |
| `func main` 可以加个参数来接命令行参数 | 入口由运行时调用，签名被定死为无参数无返回值 | 命令行参数从 `os.Args` 读取，[第 12 讲](/foundation/capstone) 会用 `flag` 包 |
| `import` 后面写的就是包名 | 那是导入路径，等于模块路径加目录；包名是包内部声明的那个名字 | 两者通常一致但并非必然，冲突时用别名区分 |
| `Println` 和 `Print` 差不多，换行无所谓 | `Println` 自动在参数间加空格、结尾补换行，`Print` 两者都不做 | 需要精确控制输出时用 `Printf`，需要自动换行时用 `Println` |
| 首字母大写只是命名习惯 | 它是 Go 唯一的跨包可见性机制，没有 `public` 关键字 | 小写标识符只在包内可见，写错会导致编译失败 |

其中第一条可以配一组命令对照，体会"产物留不留"这个唯一差别：

```bash
# go run：编译到临时目录，执行完即清理，当前目录不留文件
go run main.go

# go build：产物明确留下，可反复执行、可分发
go build -o hello main.go && ./hello
```

### 版本与平台边界

| 边界 | 具体差异 | 怎么处理 |
| --- | --- | --- |
| Go 1.21 | 内置 `min`、`max`，本讲练习里用到 | 版本低于 1.21 时需自己实现 |
| Go 1.22 | 循环变量语义调整 | 只影响循环相关代码，拆解见 [第 15 讲](/basics/range-pitfalls) |
| 产物后缀 | Windows 是 `hello.exe`，macOS 与 Linux 无后缀 | 构建脚本按 GOOS 拼名字 |
| 交叉编译 | 默认按本机 GOOS、GOARCH 产出 | 用 `GOOS=linux GOARCH=amd64 go build` 覆盖 |
| 给程序传参 | `go run main.go -x` 里的 `-x` 会被当成 go 命令自己的参数 | 写成 `go run main.go -- -x`，双横线之后才交给程序 |
| 格式串排版 | `gofmt` 的重排规则随工具链版本演进 | 升级 Go 后出现大面积 diff 属正常，先看清再提交 |

最后两行是新手最常撞上的两处：`go run` 传参要加 `--`，以及升级工具链后 `gofmt` 结果变化并不代表代码被改坏了。

<Predict q="执行 go run main.go 之后，当前工作目录里会多出什么？" :options='["一个名为 main 的可执行文件","什么都不会留下，产物在系统临时目录且已清理","一个 main.o 目标文件","一个新的 go.mod"]' answer="1" output="当前目录的文件列表保持不变" explain="go run 把编译产物放在临时目录，运行结束即清理；想留下可分发的二进制必须用 go build。这一条正是两条命令最本质的差别。" />

## 4. 工程实践

真实项目里，两条命令分工很明确：本地写逻辑用 `go run`，CI 与发布一律用 `go build`，并且用 `-o` 给出确定的产物名，避免默认用目录名导致脚本难写。交叉编译是 Go 最省心的能力之一——不用装目标系统的工具链，临时覆盖两个环境变量即可：

```bash
go run main.go
go build -o hello main.go
./hello
GOOS=linux GOARCH=amd64 go build -o hello-linux main.go
```

<Callout type="tip" title="别把二进制提交进仓库" text="go build 的产物是构建输出，应当写进 .gitignore 由 CI 重新生成，而不是提交到版本库。仓库里只放源码、go.mod 与 go.sum；这也是第 02 讲强调可复现构建的原因——任何人都能从源码重建出同一份产物。" />

还有个常见困惑：为什么 `go run` 感觉比 `go build` 后直接执行慢一点？因为它每次都要走一遍编译流程，只是结果放在临时目录里。Go 有构建缓存，重复编译同一份代码会快很多，但"每次都要启动一次编译"这件事本身不变。所以循环压测、跑基准测试时，务必先 `go build` 再反复执行二进制，别拿 `go run` 的耗时当性能数据。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l0-guide/lesson-003-hello`](https://github.com/lfire/go/tree/main/examples/l0-guide/lesson-003-hello)。clone 仓库后进入 `examples/l0-guide`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="让程序打印两行" task="改造最小程序：用两次 fmt.Println，一行打印 40 + 2 的结果，另一行打印 7 * 6 的结果。先想清楚两行分别是什么，再运行核对。" :hints='["Println 每调用一次就占一行，不必自己写换行","两个表达式都可以直接作为参数，Go 会先算出值"]' solution="func main() {\n    fmt.Println(40 + 2)\n    fmt.Println(7 * 6)\n}" explain="两行都输出 42。这个练习的重点是体会：Println 自带换行，且参数可以是任意表达式。" />

## 随堂检测

<Quiz no="3-1" q="关于这个最小程序，下面哪一项说法是错误的？" :options='["package main 表示要生成可执行文件","func main 可以带一个参数用来接收命令行参数","首字母大写的 Println 是导出标识符","未使用的 import 会导致编译失败"]' answer="1" explain="main 函数固定无参数、无返回值；命令行参数要从 os.Args 读取，不能靠给 main 加参数。" />

<Predict no="3-2" q="第三段代码里 fmt.Printf 用 %.2f 打印 math.Pi，输出是什么？" :options='["圆周率约等于 3.14","圆周率约等于 3.141592653589793","圆周率约等于 3","编译报错"]' answer="0" explain="%.2f 表示按浮点数保留两位小数并四舍五入，3.14159 变成 3.14，且格式串里的换行由末尾的 n 转义提供。" />

<FillBlank no="3-3" tpl="package ___ 声明可运行程序\n用 ___ 关键字定义函数\n用 ___ 关键字导入外部包" :answers='["main","func","import"]' hint="三个空分别是包名、定义函数的关键字、导入包的关键字。" explain="package main、func、import 共同构成每个 Go 程序的骨架，第 01 讲见过第一个，这里补齐另外两个。" :explains='["可执行程序的包名固定为 main，编译器据此把它链接成可执行文件而非库；换成别的名字只会产出包归档。","定义函数必须用 func 开头，入口函数同样名为 main；包名与函数名配套，链接器才找得到程序启动点。","代码里用到 fmt 等标准库时，必须先用 import 声明依赖，否则编译器会因为找不到标识符而报错。"]' />

## 一页纸速记

<KnowledgeCard :points='["package main 加 func main 构成一个可执行程序，main 固定无参数无返回值","import 声明依赖，多个导入用括号分组；未使用的 import 与局部变量都是编译错误","首字母大写是导出标识符，fmt.Println 正是靠这条规则被外部调用","go run 编译到临时目录并立即执行，go build 在当前目录留下可分发的二进制"]' :pitfalls='["别以为 go run 是解释执行，它每次都完整编译一遍","别把 go build 的二进制提交进版本库","别拿 go run 的耗时做性能判断，压测要先 build"]' summary="看懂这四行，就掌握了 Go 组织代码、暴露接口和交付产物的三套基本约定。" />

## 小结与下一讲

- 最小程序的四行分别负责：声明包、声明依赖、定义入口、调用标准库打印。
- 编译器刻意严格：未使用的导入和变量直接报错，首字母大小写决定可见性。
- `go run` 用于快速迭代，`go build` 用于交付；交叉编译只需覆盖 GOOS 与 GOARCH。

单个程序已经跑通，接下来认识让日常开发变顺手的一整套命令。

[下一讲：工具链全景：build / test / fmt / vet / mod](/guide/toolchain)
