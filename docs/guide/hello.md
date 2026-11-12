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

## 动手练一练

<Exercise title="让程序打印两行" task="改造最小程序：用两次 fmt.Println，一行打印 40 + 2 的结果，另一行打印 7 * 6 的结果。先想清楚两行分别是什么，再运行核对。" :hints='["Println 每调用一次就占一行，不必自己写换行","两个表达式都可以直接作为参数，Go 会先算出值"]' solution="func main() {\n    fmt.Println(40 + 2)\n    fmt.Println(7 * 6)\n}" explain="两行都输出 42。这个练习的重点是体会：Println 自带换行，且参数可以是任意表达式。" />

## 随堂检测

<Quiz no="03-1" q="关于这个最小程序，下面哪一项说法是错误的？" :options='["package main 表示要生成可执行文件","func main 可以带一个参数用来接收命令行参数","首字母大写的 Println 是导出标识符","未使用的 import 会导致编译失败"]' answer="1" explain="main 函数固定无参数、无返回值；命令行参数要从 os.Args 读取，不能靠给 main 加参数。" />

<Predict no="03-2" q="第三段代码里 fmt.Printf 用 %.2f 打印 math.Pi，输出是什么？" :options='["圆周率约等于 3.14","圆周率约等于 3.141592653589793","圆周率约等于 3","编译报错"]' answer="0" explain="%.2f 表示按浮点数保留两位小数并四舍五入，3.14159 变成 3.14，且格式串里的换行由末尾的 n 转义提供。" />

<FillBlank no="03-3" tpl="package ___ 声明可运行程序\n用 ___ 关键字定义函数\n用 ___ 关键字导入外部包" :answers='["main","func","import"]' hint="三个空分别是包名、定义函数的关键字、导入包的关键字。" explain="package main、func、import 共同构成每个 Go 程序的骨架，第 01 讲见过第一个，这里补齐另外两个。" />

## 一页纸速记

<KnowledgeCard :points='["package main 加 func main 构成一个可执行程序，main 固定无参数无返回值","import 声明依赖，多个导入用括号分组；未使用的 import 与局部变量都是编译错误","首字母大写是导出标识符，fmt.Println 正是靠这条规则被外部调用","go run 编译到临时目录并立即执行，go build 在当前目录留下可分发的二进制"]' :pitfalls='["别以为 go run 是解释执行，它每次都完整编译一遍","别把 go build 的二进制提交进版本库","别拿 go run 的耗时做性能判断，压测要先 build"]' summary="看懂这四行，就掌握了 Go 组织代码、暴露接口和交付产物的三套基本约定。" />

## 小结与下一讲

- 最小程序的四行分别负责：声明包、声明依赖、定义入口、调用标准库打印。
- 编译器刻意严格：未使用的导入和变量直接报错，首字母大小写决定可见性。
- `go run` 用于快速迭代，`go build` 用于交付；交叉编译只需覆盖 GOOS 与 GOARCH。

单个程序已经跑通，接下来认识让日常开发变顺手的一整套命令。

[下一讲：工具链全景：build / test / fmt / vet / mod](/guide/toolchain)
