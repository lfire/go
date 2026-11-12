---
title: 第 12 讲 · 阶段综合：CLI 单位换算器
description: 把地基知识串成一个能跑、能用、能测的命令行程序
---

<StageHeader stage="L1 · 编程地基" level="★★☆☆☆" time="40 分钟" goal="把地基知识串成一个能用的命令行程序" prereq="第 11 讲" />

# 第 12 讲 · 阶段综合：CLI 单位换算器

> 十一讲的概念如果不落到一处，就只是散落的名词。这一讲做一个真正能用的命令行单位换算器，把变量与常量、显式转换、函数与多返回值、包与文件组织、错误返回全部串起来，并且给它写上表格驱动测试。更重要的是体会一种分工：主流程只负责接线，纯逻辑单独放、单独测。这是后面所有项目结构的雏形。

## 这一讲你会得到什么

<CheckList id="L1-12" title="本讲目标" :items='["会用 flag 包解析命令行参数，并能判断参数是否合法","能把输入校验写成返回结果加错误的纯函数","会写表格驱动测试，加用例不用改测试逻辑","能搭出 main 接线、逻辑分层、测试独立的小项目结构"]' />

## 1. 先建立直觉

一个命令行程序拆到底只有四步：**读参数 → 校验 → 换算 → 输出**。新手常把这四步全塞进 `main`，结果是逻辑没法单独测——想验证"3 公里等于 3000 米"，得先伪造一整套命令行参数。老练的写法是把中间两步抽成不碰终端、不碰全局状态的纯函数，`main` 只做"解析参数、调用函数、打印结果"这三件接线活。

<ConceptDiagram type="layout" title="分层：main 只接线，纯逻辑与测试各自独立" />

<ConceptDiagram type="test-pyramid" title="表格驱动单测位于最厚、最快的底座" />

第二张图说明测试为什么要放在纯函数这一层：单元测试多而快，靠它兜住换算规则；至于命令行进来后参数有没有被正确解析，留少量集成检查即可。底座越厚，反馈越快、定位越准。

## 2. 语法与规则

先看参数解析。`flag` 包为每个参数返回一个**指针**，读取时要用 `*` 解引用：

```go
package main

import (
	"flag"
	"fmt"
)

func main() {
	from := flag.String("from", "km", "源单位")     // 字符串参数，默认值 km
	to := flag.String("to", "m", "目标单位")        // 默认值 m
	value := flag.Float64("value", 1, "要换算的数值") // 浮点参数，默认值 1
	flag.Parse() // 必须在读取参数之前调用
	fmt.Println("源：", *from, "目标：", *to, "数值：", *value)
}
```

<OutputBox title="直接运行（不带参数，用默认值）" output="源： km 目标： m 数值： 1" />

`flag.String`、`flag.Float64`、`flag.Bool`、`flag.Int` 分别对应不同类型的参数，第三个参数是 `-h` 帮助里的说明文字；`flag.Parse()` 一定要在读取之前调用，否则拿到的只有默认值。带参数运行时写成 `go run . -from km -to m -value 2.5`，参数名前面的短横线不能省。

第二段把校验写成"结果加错误"的纯函数，这是第 10 讲多返回值的直接用场：

```go
package main

import (
	"errors"
	"fmt"
)

// unitFactor 把单位名换算成相对米的倍率，不认识的单位返回错误
func unitFactor(unit string) (float64, error) {
	if unit == "km" {
		return 1000, nil
	}
	if unit == "m" {
		return 1, nil
	}
	if unit == "cm" {
		return 0.01, nil
	}
	return 0, errors.New("未知单位，只支持 km / m / cm")
}

func main() {
	f, err := unitFactor("km")
	if err != nil {
		fmt.Println("失败：", err)
		return
	}
	fmt.Println("km 的倍率：", f)

	_, err = unitFactor("inch") // 故意给一个不认识的单位
	if err != nil {
		fmt.Println("如预期报错：", err)
	}
}
```

<OutputBox title="运行结果" output="km 的倍率： 1000\n如预期报错： 未知单位，只支持 km / m / cm" guess="true" />

把校验放在纯函数里，而不是在 `main` 里直接打印退出，好处是调用方能自己决定怎么处理错误：命令行可以打到标准错误并设退出码，测试可以断言"这个输入必须报错"。未知单位与负数都由同一个入口挡住，规则集中、不会漏。

第三段把前面的零件组装成完整程序：

```go
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

func unitFactor(unit string) (float64, error) {
	if unit == "km" {
		return 1000, nil
	}
	if unit == "m" {
		return 1, nil
	}
	if unit == "cm" {
		return 0.01, nil
	}
	return 0, errors.New("未知单位，只支持 km / m / cm")
}

// convert 把 value 从 from 单位换算到 to 单位
func convert(value float64, from string, to string) (float64, error) {
	if value < 0 {
		return 0, errors.New("长度不能为负")
	}
	f, err := unitFactor(from)
	if err != nil {
		return 0, err
	}
	t, err := unitFactor(to)
	if err != nil {
		return 0, err
	}
	return value * f / t, nil
}

func main() {
	from := flag.String("from", "km", "源单位：km / m / cm")
	to := flag.String("to", "m", "目标单位：km / m / cm")
	value := flag.Float64("value", 1, "要换算的数值")
	flag.Parse()

	result, err := convert(*value, *from, *to)
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err) // 错误走标准错误
		os.Exit(1)                          // 非零退出码告诉调用方失败
	}
	fmt.Printf("%.2f %s = %.4f %s\n", *value, *from, result, *to)
}
```

<LineByLine code="result, err := convert(*value, *from, *to)\nif err != nil {\n    fmt.Fprintln(os.Stderr, err)\n    os.Exit(1)\n}\nfmt.Println(*value, *from, result, *to)" :notes='["把纯函数的两个返回值接住：结果与错误","先判错，这是 Go 处理失败的标准顺序","错误写到标准错误，别和正常结果混在一起","用非零退出码让脚本或 CI 能判断失败","正常时输出结果；完整程序里这一步用 Printf 控制小数位"]' />

<OutputBox title="直接运行（默认值）" output="1.00 km = 1000.0000 m" guess="true" />

`os.Exit(1)` 是命令行程序和外部世界的约定：退出码为 0 表示成功，非零表示失败，脚本可以据此决定要不要继续。注意 `os.Exit` 不会执行 `defer`（第 10 讲初识过 defer），所以资源清理要在它之前完成。

## 3. 深入一步

<Term word="表格驱动测试" text="把一组输入与期望输出列成表，用同一个循环逐个验证；新增用例只需加一行数据。" />

给纯函数写测试，最省力的就是表格驱动。把下面这段保存为 `convert_test.go`，用 `go test` 运行：

```go
package main

import (
	"errors"
	"testing"
)

// 真实项目里 unitFactor 在 convert.go；这里放进同一段，方便整段直接运行
func unitFactor(unit string) (float64, error) {
	if unit == "km" {
		return 1000, nil
	}
	if unit == "m" {
		return 1, nil
	}
	if unit == "cm" {
		return 0.01, nil
	}
	return 0, errors.New("未知单位")
}

func TestUnitFactor(t *testing.T) {
	cases := []struct {
		name string
		unit string
		want float64
	}{
		{"公里", "km", 1000},
		{"米", "m", 1},
		{"厘米", "cm", 0.01},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := unitFactor(tc.unit)
			if err != nil {
				t.Fatalf("意外报错：%v", err)
			}
			if got != tc.want {
				t.Errorf("得到 %v，想要 %v", got, tc.want)
			}
		})
	}
}
```

`cases` 把用例列成一张表，`for range` 逐个取出，`t.Run` 给每个用例起独立的子测试名，失败时能直接定位到是哪一行数据。想加一个单位，只要往表里加一行，测试逻辑一个字都不用改。这里的 `[]struct{}` 是切片与结构体的组合写法，完整语法分别在阶段二第 13 讲和第 17 讲展开，本讲只要看懂"一张表加一个循环"就够。

| `flag` 函数 | 参数类型 | 返回的指针类型 |
| --- | --- | --- |
| `flag.String` | 字符串 | `*string` |
| `flag.Int` | 整数 | `*int` |
| `flag.Float64` | 浮点数 | `*float64` |
| `flag.Bool` | 布尔 | `*bool` |

<Callout type="warn" title="四个容易翻车的地方" text="第一，flag.Parse 漏写或在读取之后才写，读到的永远是默认值。第二，忘记对返回的指针解引用，打印出来的是一个地址。第三，把错误只用 fmt.Println 打到标准输出，会污染正常结果、让调用方难以判断成败；应写 os.Stderr 并配非零退出码。第四，校验只写在 main 里，测试就够不着了——凡是想验证的规则，都该放进纯函数。" />

<Compare :panels='[{"label":"全塞进 main","code":"func main() {\n    flag.Parse()\n    校验\n    换算\n    fmt.Println(结果)\n}","note":"想测一条换算规则，得先伪造整套参数"},{"label":"分层","code":"func convert(...) (float64, error) { 纯逻辑 }\nfunc main() { 解析 调用 输出 }","note":"convert 可直接喂数据做表格驱动测试"}]' title="同一个程序，两种可测试性" />

<FlowTimeline :steps='[{"title":"解析参数","desc":"flag.Parse 后读取三个指针","tone":"blue"},{"title":"校验输入","desc":"负数与未知单位在这里被拦下","tone":"orange"},{"title":"纯函数换算","desc":"乘倍率再除目标倍率，不碰终端","tone":"green"},{"title":"输出或报错","desc":"正常写标准输出，失败写标准错误","tone":"purple"}]' direction="horizontal" />

## 4. 工程实践

把这个小程序当项目来摆，文件结构应该像这样：

```text
convert/
├── go.mod            # 模块定义
├── convert.go        # 纯逻辑：unitFactor / convert
├── convert_test.go   # 表格驱动测试
└── main.go           # 只做 flag 接线与输出
```

判断分层是否合理有个很实用的标准：**换算规则能不能不启动命令行就测出来**。能，说明逻辑与接线解耦了；不能，说明还有规则陷在 `main` 里。顺着这个标准往下走，阶段五讲项目结构与可测试性设计时你会觉得似曾相识。

日常命令也就三条，写进 `Makefile` 或 CI 即可：

```bash
go run . -from km -to m -value 2.5
go test ./...
go build -o convert .
```

<CodeTabs :tabs='[{"label":"正常换算","code":"go run . -from km -to m -value 2.5","note":"输出 2.50 km = 2500.0000 m"},{"label":"触发校验","code":"go run . -from inch -to m","note":"标准错误提示未知单位，退出码为 1"},{"label":"跑测试","code":"go test ./...","note":"表格里三个用例逐个通过"}]' title="三个场景各敲一条命令" />

再往前一步的边界也顺带说清：真实工具会支持更多单位与更友好的提示，倍率表更适合放进一个只读的映射结构（第 16 讲讲 map）；参数复杂了可以用子命令（阶段十 cobra）；但这些都属于"加数据"，不该动摇"纯逻辑加薄接线"这个骨架。先守住分层，加功能才不会把代码越改越难测。

<Callout type="tip" title="一条贯穿线" text="第 25 讲会做一个内存版 KV 存储，用的还是今天这套分工：main 只解析命令，逻辑与错误返回放在可被单测覆盖的层里。把这一讲的分层习惯练熟，后面读那个项目会轻松很多。" />

## 动手练一练

<Exercise title="把换算规则抽成纯函数" task="写一个不碰 flag、不碰输出的函数 kmToM，输入公里数，返回对应的米数。想清楚它为什么能被表格驱动测试直接调用。" :hints='["一公里是一千米，函数体只需要一次乘法","签名写成 func kmToM(km float64) float64"]' solution="func kmToM(km float64) float64 {\n    return km * 1000\n}" explain="纯函数不读参数、不打印、不退出，测试可以直接喂 1、2.5、0 这些值并断言结果；负数校验再加一个返回 error 的版本即可，这正是分层让代码可测的原因。" />

## 随堂检测

<Quiz no="12-1" q="关于这个 CLI 程序，下面哪一项说法是错误的？" :options='["flag.String 返回的是指针，读取时要解引用","flag.Parse 必须在读取参数之前调用","把校验放进 main 里最有利于单元测试","失败时写 os.Stderr 并配非零退出码是常见约定"]' answer="2" explain="校验放进 main 会让测试必须伪造整套参数；抽成纯函数才好做表格驱动测试。" />

<Predict no="12-2" q="完整程序直接运行、不带任何参数，会输出什么？" :options='["1.00 km = 1000.0000 m","2.50 km = 2500.0000 m","什么也不输出","直接报错退出"]' answer="0" explain="三个参数都有默认值 km、m、1，换算结果为 1000 米，按格式串保留对应小数位输出。" />

<FillBlank no="12-3" tpl="解析命令行参数前必须先调用 flag.___()\n表格驱动测试里用 t.___ 给每个用例起子测试名\n错误输出通常写到 os.___" :answers='["Parse","Run","Stderr"]' hint="三空分别是解析函数名、子测试方法名、标准错误的常量名。" explain="flag.Parse 完成解析，t.Run 建立具名子测试，os.Stderr 是标准错误而非标准输出。" />

## 一页纸速记

<KnowledgeCard :points='["命令行程序 = 读参数、校验、换算、输出；flag 返回一个指针，Parse 必须先在","校验与换算抽成结果加 error 的纯函数，main 只做接线与输出","表格驱动测试用一张表加一个循环，加用例不改逻辑","失败写 os.Stderr 并配非零退出码；main 接线、逻辑、测试分文件摆放"]' :pitfalls='["别漏写 flag.Parse，也别忘了对参数指针解引用","别把错误打到标准输出污染正常结果","别把校验规则塞进 main，否则单测够不着"]' summary="把可测的纯逻辑与薄薄的一层接线分开，是地基阶段所有知识最终汇成的那一条工程习惯。" />

## 小结与下一讲

- `flag` 负责读参数，返回指针；`flag.Parse()` 必须先调用，帮助与默认值由它统一处理。
- 校验与换算写成 `(结果, error)` 纯函数，`main` 只解析、调用、输出；失败走标准错误与非零退出码。
- 表格驱动测试用一张用例表覆盖多条规则；项目按 main、逻辑、测试分文件，依赖方向保持单向。

编程地基到此收口。下一阶段从最常用的序列结构开始，先看数组与切片。

[下一讲：数组与切片：定义与操作](/basics/arrays-slices)
