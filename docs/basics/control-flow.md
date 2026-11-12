---
title: 第 14 讲 · 控制流：if / for / switch / goto
description: 掌握 Go 精简而独特的控制结构，理解 switch 不穿透、唯一的 for 与 goto 的边界
---

<StageHeader stage="L2 · 语法核心" level="★☆☆☆☆" time="30 分钟" goal="掌握 Go 精简而独特的控制结构" prereq="第 13 讲 · 数组与切片：定义与操作" />

# 第 14 讲 · 控制流：if / for / switch / goto

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

## 动手练一练

<Exercise title="跳过偶数求和" task="用 for 与 continue 求 1 到 10 中所有奇数的和并打印。" :hints='["用一个 sum 累加","偶数时 continue"]' solution="sum := 0\nfor _, i := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10} {\n    if i%2 == 0 {\n        continue\n    }\n    sum += i\n}\nfmt.Println(sum)" explain="偶数被跳过，只累加 1、3、5、7、9，输出 25。" />

## 随堂检测

<Quiz no="14-1" q="关于 Go 的 switch，正确的是？" :options='["命中 case 后会继续穿透","fallthrough 可写在最后一个 case","无条件 switch 的 case 可写布尔表达式","switch 条件必须加括号"]' answer="2" explain="命中即结束；fallthrough 不能用于最后一个 case；条件不加括号。" />

<Predict no="14-2" q="day 为 3，case 3 末尾有 fallthrough，紧跟 case 4 打印「邻近周四」，输出是？" :options='["只有 周三","周三 与 邻近周四","只有 邻近周四","周三 与 其它"]' answer="1" output="周三\n邻近周四" explain="fallthrough 执行下一个 case 后 switch 结束，default 不再执行。" />

## 一页纸速记

<KnowledgeCard :points='["if 初始化语句的变量只在链内可见","三种 for 循环共用同一个关键字","switch 自动 break，fallthrough 才继续","type switch 按动态类型分支"]' :pitfalls='["else 另起一行","误以为 case 会穿透","无条件 for 没有 break"]' summary="分支选路径，循环重复路径；switch 不穿透。" />

## 小结与下一讲

- `if` 的初始化语句让临时变量不外泄；条件不加括号，花括号必须写。
- `for` 是唯一循环关键字，三种形式覆盖计数、条件与无限循环。
- `switch` 命中即结束，`fallthrough` 需显式声明；`goto` 只用于跳出多层嵌套。

循环里最常用的 `range` 还没展开，而它恰恰藏着 Go 最经典的面试陷阱。

[下一讲：for range 与闭包捕获陷阱](/basics/range-pitfalls)
