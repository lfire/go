---
title: 第 014 讲 · 控制流：if / for / switch / goto
description: 掌握 Go 精简而独特的控制结构，理解 switch 不穿透、唯一的 for 与 goto 的边界
---

<StageHeader stage="L2 · 语法核心" level="★☆☆☆☆" time="30 分钟" goal="掌握 Go 精简而独特的控制结构" prereq="第 013 讲 · 数组与切片：定义与操作" />

# 第 014 讲 · 控制流：if / for / switch / goto

> 前面的程序都一条直线走到底。真实逻辑要能「看情况」和「重复做」：满足条件才执行、不满足换一条路、同一段代码跑很多次。Go 把控制流收得很紧——只有一种循环关键字 `for`，`switch` 默认不穿透。学完本讲，你能写出结构清晰、不被其他语言习惯带偏的分支与循环。

## 这一讲你会得到什么
<CheckList id="l14-goals" title="本讲目标" :items='["会用 if 初始化语句并说清作用域","写出 for 三种形式与 break/continue","记住 switch 不穿透与 type switch","知道 goto 的适用边界"]' />

## 1. 先建立直觉

把程序看成在一条路上行走：默认一条条往下走；`if` 与 `switch` 是岔路口，按条件选一条；`for` 是环形跑道，跑完一圈回到起点再判断是否继续；`goto` 是直达电梯，只在少数多层嵌套时用来一次跳到出口。Go 刻意只留一种循环写法，就是让你不必在 `while`、`do-while`、`for` 之间纠结。

<ConceptDiagram type="hello-flow" title="控制流改变执行顺序" labels="顺序::依次执行|分支::if 与 switch|循环::唯一的 for|汇合::继续向下" caption="分支选路径，循环重复路径" />

<FlowTimeline title="三段式 for 的执行链" :steps='[{"title":"初始化","desc":"只跑一次","tone":"blue"},{"title":"条件判断","desc":"为真才进","tone":"orange"},{"title":"循环体","desc":"可 break 或 continue","tone":"green"},{"title":"后置语句","desc":"更新后再判断","tone":"purple"}]' direction="horizontal" />

## 2. 语法与规则

先看 `if`。条件不加括号，花括号必须写；条件前可以放一句初始化语句，声明的变量只在整条 `if/else` 链里有效。这个设计让「先取值、再判断」的临时变量不污染外层作用域。

```go
package main

import "fmt"

func main() {
	// n 只在 if/else 链内可见
	if n := 7; n%2 == 0 {
		fmt.Println("偶数")
	} else {
		fmt.Println("奇数", n)
	}
	// fmt.Println(n) // 放到这里会编译失败

	score := 85
	if score >= 60 {
		fmt.Println("及格")
	}
}
```

输出里 `n` 只在分支内可用，离开即失效。这正是 Go 用作用域换可读性的典型取舍。

接着是唯一的循环关键字 `for`：三段式、只有条件（相当于别的语言 `while`）、无条件配合 `break`。`continue` 直接跳到下一轮。

```go
package main

import "fmt"

func main() {
	for i := 0; i < 3; i++ {
		fmt.Print(i, " ")
	}
	fmt.Println()

	sum, n := 0, 1
	for n <= 5 {
		sum += n
		n++
	}
	fmt.Println("1 到 5 的和 =", sum)

	for {
		fmt.Println("执行一次就退出")
		break
	}

	for i := 0; i < 5; i++ {
		if i%2 == 0 {
			continue
		}
		fmt.Print(i, " ")
	}
	fmt.Println()
}
```

<OutputBox title="运行结果" output="0 1 2 \n1 到 5 的和 = 15\n执行一次就退出\n1 3 " />

<LineByLine code="if n := 7; n%2 == 0 {\n    fmt.Println(n)\n} else {\n    fmt.Println(n)\n}" :notes='["初始化语句声明的 n 只在链内可见","条件成立时打印本轮的 n","else 须紧跟右花括号","条件不成立时同样能访问 n","右花括号结束语句，n 随之失效"]' title="if 初始化语句逐行拆解" />

还有第四种形式 `range`，用于遍历切片、map、字符串和通道。它很常用，也藏着经典陷阱，下一讲专门拆解，这里只要知道它属于 `for` 家族。

## 3. 深入一步

`switch` 是 Go 与 C 系语言差别最大的地方：命中一个 `case` 后自动结束，不会向下执行。想继续必须显式写 `fallthrough`。它还可以完全不带表达式，把每个 `case` 写成布尔条件，替代一长串 `if-else`；配合 `any` 还能做 `type switch`，按动态类型分支。

```go
package main

import "fmt"

func main() {
	// 命中即结束；fallthrough 才继续下一个 case
	switch day := 3; day {
	case 1, 2:
		fmt.Println("工作日")
	case 3:
		fmt.Println("周三")
		fallthrough
	case 4:
		fmt.Println("邻近周四")
	default:
		fmt.Println("其它")
	}

	score := 72
	switch {
	case score >= 90:
		fmt.Println("优")
	case score >= 60:
		fmt.Println("及格")
	default:
		fmt.Println("不及格")
	}

	var x any = 3.14
	switch v := x.(type) {
	case int:
		fmt.Println("整数", v)
	case string:
		fmt.Println("字符串", v)
	default:
		fmt.Println("其它类型", v)
	}
}
```

<OutputBox title="运行结果" output="周三\n邻近周四\n及格\n其它类型 3.14" />

| 形式 | 写法 | 适用场合 |
| --- | --- | --- |
| 三段式 for | `for init; cond; post` | 已知次数、需要下标 |
| 条件 for | `for cond` | 次数不定，相当于 while |
| 无条件 for | `for { ... }` | 靠 break 退出的循环 |
| range for | `for k, v := range x` | 遍历容器，下一讲细讲 |
| 表达式 switch | `switch x { case ... }` | 单值多分支 |
| 无条件 switch | `switch { case cond }` | 替代 if-else 链 |

为什么让 switch 自动结束？因为绝大多数 case 本就独立，穿透只是少数需求；把常见情况设为默认，能省掉满屏 break，也消除了忘记写 break 的隐蔽 bug。这和「只有一种 for」是同一套思路：减少选择，把正确性交给语法。

<Term word="fallthrough" text="继续执行紧邻的下一个 case，只能写在 case 末尾，最后一个 case 不能用。" />

<Callout type="warn" title="易错点" text="别把 C 的习惯带进来：case 不自动穿透，多个值写在同一 case 用逗号分隔；fallthrough 不判断下一 case 的条件。" />

`goto` 主要用于一次跳出多层循环或集中做清理，但有硬规则：不能跳过变量声明跳进某个作用域，也不能跨函数。滥用会让执行路径无法追踪，只有带标志位的 `break` 明显更啰嗦时才考虑。

```go
package main

import "fmt"

func main() {
	found := false
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if i == 1 && j == 2 {
				found = true
				goto DONE // 一次跳出两层循环
			}
		}
	}
DONE:
	fmt.Println("结束，found =", found)
}
```

<OutputBox title="运行结果" output="结束，found = true" />

## 4. 工程实践

写分支优先用「早返回」代替深层嵌套：先处理边界条件并 `return`，主流程就能停在一个缩进层级。多条件排队时无条件 `switch` 比一串 `else if` 易读，`case` 顺序天然表达优先级。循环里务必确认终止条件会变化，无条件 `for` 必须有可达的 `break`，否则就是死循环。

<Compare title="switch 的两种走向" :panels='[{"label":"写法 A","code":"switch day {\ncase 1:\n    handle()\ncase 2:\n    handle()\n}","note":"命中即结束，不穿透"},{"label":"写法 B","code":"switch day {\ncase 1:\n    handle()\n    fallthrough\ncase 2:\n    handle()\n}","note":"显式 fallthrough 连带执行 case 2"}]' />

<Callout type="tip" title="格式即语法" text="else 必须紧跟 if 的右花括号，否则自动插入分号导致编译失败；这些由 gofmt 统一保证。" />

为什么 Go 敢把三种循环压成一个 `for`？因为编译器眼里它们本就是同一套控制流：初始化、条件、循环体、后置语句四个块，缺哪块就留空，`for cond` 相当于初始化和后置为空，`for {}` 相当于条件恒为真。可用 `go build -gcflags=-S` 编译两段等价代码，会发现生成的跳转指令几乎一样，差别只在条件块是否为空。同理 `switch` 命中即结束也不是特例，而是把每个 `case` 编译成一段带隐式跳转的代码；想看它长什么样，`go vet` 会顺带报出 `unreachable code`——在 `goto` 之后或无限 `for` 之后写的语句，工具能直接替你查出来。想从语法层确认结构，可以 `go doc go/ast.IfStmt`、`go doc go/ast.ForStmt` 看抽象语法树的字段：`if` 的 `Init` 字段正是那条初始化语句，所以它的作用域被设计成覆盖整条 `if/else` 链，等价于编译器在外层悄悄包了一个块。

真实踩坑里最高频的是分号：`else` 必须紧跟 `if` 的右花括号，另起一行时 Go 的自动分号插入会在 `}` 后补一个分号，`else` 就成了没有归属的语句，直接编译失败——`gofmt` 会强制把它拉回来，这也是「格式即语法」的由来。另一类坑更隐蔽：`for {}` 里漏写可达的 `break`，程序会占满一个 CPU 核，用 `top` 能看到进程 100%；这类死循环 `go vet` 不一定报得出来，只能靠循环条件会变化的自检。还有一个 C 转过来的习惯要戒：在 `case` 末尾习惯性写 `break`，Go 里它只会跳出包住 switch 的那层 `for`（如果有的话），并不会结束 switch——不写反而才是想要的。

<FlowTimeline title="if 初始化语句的生命周期" :steps='[{"title":"初始化","desc":"n, err := 求值","tone":"blue"},{"title":"条件","desc":"err == nil ?","tone":"orange"},{"title":"分支体","desc":"n 与 err 都可见","tone":"green"},{"title":"链结束","desc":"变量随之失效","tone":"purple"}]' direction="horizontal" />

把两条边界写成可运行的代码。第一段用 `strconv.Atoi` 配 `if` 初始化语句，展示错误分支里变量依然可见、出链即失效。

```go
package main

import (
	"fmt"
	"strconv"
)

func main() {
	// if 初始化语句最典型的用途：先取值和错误，再按错误分支
	if n, err := strconv.Atoi("42"); err == nil {
		fmt.Println("解析成功，n =", n)
	} else {
		fmt.Println("解析失败，n 取零值 =", n)
	}

	// 错误分支里 n、err 依然可见；出了整条链就失效
	if n, err := strconv.Atoi("abc"); err != nil {
		fmt.Println("非法输入被挡住，n =", n, "有错 =", err != nil)
	}
}
```

<OutputBox title="运行结果" output="解析成功，n = 42\n非法输入被挡住，n = 0 有错 = true" />

第二段用带标签的 `break` 一次跳出两层循环，比 `goto` 更好读；同时把「没找到」当成明确结果返回，而不是把零值下标误当成命中，并验证 nil 输入不会 panic。

```go
package main

import "fmt"

// find 在二维表里找 target，返回行列与是否命中。
// 用标签 break 一次跳出两层循环，比 goto 更清晰；
// 没命中要靠第三个返回值明确区分，别把 0 行列当成功。
func find(grid [][]int, target int) (int, int, bool) {
	ri, rj := -1, -1
FOUND:
	for i, row := range grid {
		for j, v := range row {
			if v == target {
				ri, rj = i, j
				break FOUND
			}
		}
	}
	if ri == -1 {
		return 0, 0, false
	}
	return ri, rj, true
}

func main() {
	grid := [][]int{{1, 2}, {3, 4}}
	if i, j, ok := find(grid, 4); ok {
		fmt.Println("找到于", i, j)
	}
	if _, _, ok := find(grid, 99); !ok {
		fmt.Println("没找到 99，别把零值下标当成命中")
	}
	if _, _, ok := find(nil, 1); !ok { // nil 表格也安全
		fmt.Println("nil 输入也安全")
	}
}
```

<OutputBox title="运行结果" output="找到于 1 1\n没找到 99，别把零值下标当成命中\nnil 输入也安全" />

| 语句 | 作用范围 | 常见坑 |
| --- | --- | --- |
| `break` | 跳出最近一层 for / switch / select | 嵌套循环里只跳一层，跳多层得用标签 |
| `continue` | 跳到最近一层 for 的下一轮 | 写在 switch 里不会跳出 for，需要标签 |
| `fallthrough` | 进入紧邻的下一个 case | 不判断下一 case 条件，也不能放最后 |
| `goto Label` | 跳到本函数内的标签 | 不能跳过变量声明、不能跨函数 |
| `break Label` | 跳出标签所指的那一层循环 | 标签必须紧贴被跳的循环 |

<Predict q="用 if 初始化语句解析一个非法数字字符串，err 不为 nil 时进入对应分支，此时同名变量 n 的值和可见性是？" :options='["n 为 0 且只在链内可见","n 为解析前的值且函数内都可见","编译失败，err 分支里不能用 n","n 为 0 且函数内都可见"]' answer="0" output="n = 0，且 n 与 err 只在整条 if/else 链内可见" explain="Atoi 失败时返回 int 零值 0；初始化语句声明的 n、err 作用域覆盖整条链，离开右花括号即失效，这正是它不污染外层的价值。" />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l2-basics/lesson-014-control-flow`](https://github.com/lfire/go/tree/main/examples/l2-basics/lesson-014-control-flow)。clone 仓库后进入 `examples/l2-basics`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

如果还想把上面几条结论追到语言规范与编译器节点，下面给出可自己复现的推导、两条真实误解的对照，以及带逐字段解释的实验。

### 再推一层：唯一的 for 与自动结束的 switch 从哪来

「唯一的 `for`」不是把三种循环硬塞进一个关键字，而是规范里 `ForStmt` 本来就只有一套字段。`go doc go/ast.ForStmt` 显示 `Init Stmt`、`Cond Expr`、`Post Stmt` 三个字段都标注 `or nil`：三段式是三者齐全，`for cond` 是 `Init` 与 `Post` 为 nil，`for {}` 连 `Cond` 也为 nil（等价于恒真）。编译器在 `cmd/compile/internal/walk` 把它们统一降级成带条件跳转的同一类节点，所以用 `go build -gcflags=-S` 看两段等价循环，跳转指令几乎一样。`switch` 命中即结束也不是特例：规范规定每个 `case` 是独立的隐式块，执行完即终止整个 switch；`fallthrough` 只能作为某个 case 的最后一条非空语句，且不检查下一 case 的条件。`goto` 不能跳过变量声明同样写在规范「Goto statements」里：跳进某个块会让块内声明的变量处于「已进入作用域却未初始化」的状态，编译器在类型检查阶段直接报 `goto ... jumps over declaration of ...`。`if` 初始化语句的作用域覆盖整条链，则能从 `go doc go/ast.IfStmt` 的 `Else Stmt` 字段推出：`else if` 是嵌套的 `IfStmt`，整条链被包进同一个隐式块，`Init` 声明的变量因此到最末一个 else 都可见。

### 两条真实误解：错误写法与正确写法

**误解一：从 C 转过来，switch 的每个 case 末尾都要写 `break` 防止穿透。** Go 的 case 自带隐式结束；多写一个 `break` 只是冗余——`break` 只结束最内层的 `switch`，**不会**跳出外层 `for`；想跳出循环必须写带标签的 `break Label`。

<Compare title="case 末尾要不要写 break" :panels='[{"label":"冗余：照搬 C 习惯","code":"for _, day := range []int{1, 2, 3} {\n    switch day {\n    case 1:\n        handle()\n        break\n    case 2:\n        handle()\n    }\n}","note":"break 只结束 switch，外层 for 照常跑完"},{"label":"正确：什么都不写","code":"switch day {\ncase 1:\n    handle()\ncase 2:\n    handle()\n}","note":"命中即结束；要连带执行才写 fallthrough"}]' />

`break` 作用于最近的 `for`/`switch`/`select`，在 case 里最近的就是 `switch` 本身；不写才是 Go 想要的默认行为，这也是「格式即语法」之外另一条容易带偏的习惯。

**误解二：`goto` 能跳到同函数内任意标签，包括跳进内层块。** 跳过变量声明会被编译期直接拒绝。

<Compare title="goto 与变量声明" :panels='[{"label":"错误：跳过声明","code":"goto DONE\nx := 1\n_ = x\nDONE:\nprintln(\"done\")","note":"编译失败：goto DONE jumps over declaration of x"},{"label":"正确：声明放在跳转之前","code":"x := 1\ngoto DONE\n_ = x\nDONE:\nprintln(x)","note":"标签处变量已初始化，跳转合法"}]' />

把声明提前、或改用带标签的 `break`，都能绕开这条限制；真正多层跳出时带标签 break 往往比 goto 更好读。

### 可验证实验：命令、输出与逐字段解释

```bash
go doc go/ast.ForStmt
go vet ./...
```

```text
type ForStmt struct {
	For  token.Pos // position of "for" keyword
	Init Stmt      // initialization statement; or nil
	Cond Expr      // condition; or nil
	Post Stmt      // post iteration statement; or nil
	Body *BlockStmt
}
# example.com/demo
./main.go:9:2: unreachable code
```

| 输出片段 | 含义 |
| --- | --- |
| `Init Stmt / or nil` | 初始化语句可为空，这是「条件 for」的来源 |
| `Cond Expr / or nil` | 条件为空即恒真，对应无限 for |
| `Post Stmt / or nil` | 后置语句可为空，三段式退化成 while |
| `Body *BlockStmt` | 三种形式共用同一个循环体节点 |
| `# example.com/demo` | vet 报告的包路径 |
| `./main.go:9:2` | 文件、行、列：不可达语句的位置 |
| `unreachable code` | 控制流图判定该行永不执行，常见于无 break 的无限 for 之后 |

下面这段把 switch 自动结束、`fallthrough` 不判条件、if 链作用域与带标签 break 一次跑出来：

```go
package main

import (
	"fmt"
	"strconv"
)

func main() {
	for _, day := range []int{1, 2, 3} {
		switch day {
		case 1:
			fmt.Print("一 ")
		case 2:
			fmt.Print("二 ")
		default:
			fmt.Print("其它 ")
		}
	}
	fmt.Println()

	switch n := 1; n {
	case 1:
		fmt.Print("一 ")
		fallthrough
	case 99:
		fmt.Print("无条件进入 ")
	}
	fmt.Println()

	if n, err := strconv.Atoi("42"); err == nil {
		fmt.Println("if 链内 n =", n)
	} else {
		fmt.Println("else 里同样可见 n =", n)
	}

FOUND:
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if i == 1 && j == 2 {
				fmt.Println("标签 break 跳出于", i, j)
				break FOUND
			}
		}
	}
}
```

<OutputBox title="运行结果" output="一 二 其它 \n一 无条件进入 \nif 链内 n = 42\n标签 break 跳出于 1 2" />

### 版本差异与适用边界

| 版本 | 变化 | 适用边界 |
| --- | --- | --- |
| Go 1.18 | 泛型加入，但 `type switch` 的 case 不能写类型参数 | 仍是语法限制 |
| Go 1.22 | 循环变量每轮新建，由 `go.mod` 的 `go` 指令决定 | 与第 015 讲衔接，闭包捕获语义改变 |
| Go 1.22 | 新增整数 range：`for i := range 10` | 仍是同一个 ForStmt 节点 |
| Go 1.23 | 新增函数迭代器 range：`for x := range seq` | 需 `iter.Seq`，同样是每轮新变量 |
| 全版本 | switch 自动结束、goto 不跨声明、else 紧跟右花括号 | 结论与工具链版本无关 |

无条件 `for` 里漏写可达的 `break` 会占满一个 CPU 核，这类死循环 `go vet` 不一定报得出来，只能靠「循环条件会变化」的自检；`fallthrough` 也不能放在最后一个 case。

### 与 L3 底层、L10 实战的呼应

[第 036 讲](/core/compiler) 会展开 SSA 与控制流降级，用 `go tool compile -S` 看这里说的跳转指令；[第 044 讲](/concurrency/select) 的 `case` 与 switch 一样是独立分支、`break` 语义相同。实战里，[第 108 讲](/projects/data-sql-tool) 的 AST 遍历满是 `type switch` 与多分支 switch，[第 117 讲](/projects/cloud-operator) 的 Reconcile 状态机靠 `for` 加 `switch` 驱动——把控制流写直，后面这些代码才好读。

## 动手练一练

<Exercise title="跳过偶数求和" task="用 for 与 continue 求 1 到 10 中所有奇数的和并打印。" :hints='["用一个 sum 累加","偶数时 continue"]' solution="sum := 0\nfor _, i := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10} {\n    if i%2 == 0 {\n        continue\n    }\n    sum += i\n}\nfmt.Println(sum)" explain="偶数被跳过，只累加 1、3、5、7、9，输出 25。" />

## 随堂检测

<Quiz no="014-1" q="关于 Go 的 switch，正确的是？" :options='["命中 case 后会继续穿透","fallthrough 可写在最后一个 case","无条件 switch 的 case 可写布尔表达式","switch 条件必须加括号"]' answer="2" explain="命中即结束；fallthrough 不能用于最后一个 case；条件不加括号。" />

<Predict no="014-2" q="day 为 3，case 3 末尾有 fallthrough，紧跟 case 4 打印「邻近周四」，输出是？" :options='["只有 周三","周三 与 邻近周四","只有 邻近周四","周三 与 其它"]' answer="1" output="周三\n邻近周四" explain="fallthrough 执行下一个 case 后 switch 结束，default 不再执行。" />

## 一页纸速记

<KnowledgeCard :points='["if 初始化语句的变量只在链内可见","三种 for 循环共用同一个关键字","case 自动结束（无需 break），fallthrough 才继续","type switch 按动态类型分支"]' :pitfalls='["else 另起一行","误以为 case 会穿透","无条件 for 没有 break"]' summary="分支选路径，循环重复路径；switch 不穿透。" />

## 小结与下一讲

- `if` 的初始化语句让临时变量不外泄；条件不加括号，花括号必须写。
- `for` 是唯一循环关键字，三种形式覆盖计数、条件与无限循环。
- `switch` 命中即结束，`fallthrough` 需显式声明；`goto` 只用于跳出多层嵌套。

循环里最常用的 `range` 还没展开，而它恰恰藏着 Go 最经典的面试陷阱。

[下一讲：for range 与闭包捕获陷阱](/basics/range-pitfalls)
