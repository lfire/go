---
title: 第 15 讲 · for range 与闭包捕获陷阱
description: 拆解 range 的副本语义、循环变量捕获与 Go 1.22 的语义变化，学会定位经典生产 bug
---

<StageHeader stage="L2 · 语法核心" level="★★★☆☆" time="30 分钟" goal="避开 Go 面试与生产中最经典的坑" prereq="第 14 讲 · 控制流：if / for / switch / goto" />

# 第 15 讲 · for range 与闭包捕获陷阱

> 上一讲把 `range` 留到了这里，因为它最容易写出「看起来对、跑起来错」的代码：遍历时改不动元素、把循环变量交给闭包后拿到一串相同的值、遍历 map 的顺序每次都不一样。更麻烦的是，其中一类坑在 Go 1.22 被语言层面修掉了，同一份源码用不同版本的 `go.mod` 会跑出不同结果。本讲逐个拆开并给出可复现的排查路径。

## 这一讲你会得到什么
<CheckList id="l15-goals" title="本讲目标" :items='["说清 range 给的是副本","解释循环变量捕获与修法","讲明 Go 1.22 前后语义差异","处理指针切片与 map 遍历"]' />

## 1. 先建立直觉

`range` 像一台扫描仪：先把容器拍一张快照，再逐个把元素值拷进循环变量。你拿到的不是仓库里的货物，而是复印件，改复印件动不到原件。更关键的是，早期 Go 里这台扫描仪从头到尾只用一个变量槽位，每轮覆盖新值；闭包若记住的是「槽位」而不是「值」，等循环结束再回头看，读到的全是最后一轮留下的值。

<ConceptDiagram type="slice-header" title="range 复制描述符与元素" labels="表达式::循环前求值一次|循环变量::拿到元素副本|底层数组::副本不回写" caption="循环变量的槽位语义在 Go 1.22 发生变化" />

<FlowTimeline title="一次迭代" :steps='[{"title":"求值","desc":"只算一次","tone":"blue"},{"title":"拷贝","desc":"值给循环变量","tone":"orange"},{"title":"执行体","desc":"闭包记住槽位","tone":"green"},{"title":"下一轮","desc":"覆盖槽位","tone":"purple"}]' direction="horizontal" />

## 2. 语法与规则

先看副本语义。`v` 是元素值的拷贝，改它不影响原切片；想回写必须用下标。另外 `range` 后面的表达式在进入循环前只求值一次。

```go
package main

import "fmt"

func source() []int {
	fmt.Println("source 只调用一次")
	return []int{1, 2, 3}
}

func main() {
	nums := []int{1, 2, 3}
	for _, v := range nums { // v 是副本
		v *= 10
	}
	fmt.Println("遍历后 nums =", nums)

	for i, v := range nums { // 用下标回写
		nums[i] = v * 10
	}
	fmt.Println("按下标改后 nums =", nums)

	for _, v := range source() {
		fmt.Print(v, " ")
	}
	fmt.Println()
}
```

<OutputBox title="运行结果" output="遍历后 nums = [1 2 3]\n按下标改后 nums = [10 20 30]\nsource 只调用一次\n1 2 3 " />

<LineByLine code="for _, v := range nums {\n    v = v * 10\n}\nfmt.Println(nums)" :notes='["每轮把元素值拷给 v","只改副本，原切片不受影响","循环到此结束","打印仍是原值，回写要写成 nums[i]"]' title="副本语义逐行拆解" />

再看循环变量捕获。闭包保存的是变量本身而非当时的值，任何被后续修改的变量都如此。放进 `range` 后，早期版本就会得到一串相同的结果。

```go
package main

import "fmt"

func main() {
	v := 1
	f := func() { fmt.Println("闭包读到 v =", v) }
	v = 9
	f()

	var fs []func()
	for _, x := range []int{10, 20, 30} {
		fs = append(fs, func() { fmt.Printf("%d ", x) })
	}
	for _, g := range fs {
		g()
	}
	fmt.Println()

	var ps []*int
	for _, x := range []int{4, 5, 6} {
		ps = append(ps, &x)
	}
	for _, p := range ps {
		fmt.Print(*p, " ")
	}
	fmt.Println()
}
```

<OutputBox title="Go 1.22 及之后的运行结果" output="闭包读到 v = 9\n10 20 30 \n4 5 6 " />

<Term word="每轮新变量" text="Go 1.22 起循环变量每轮各建一个实例，闭包互不影响。" />

## 3. 深入一步

Go 1.22 之前整轮循环共用一个循环变量，闭包和 `&x` 都指向同一槽位，于是上面代码的后两行在旧版本会打印 `30 30 30` 与 `6 6 6`。1.22 把 `for` 与 `range` 统一改成每轮新变量，上述写法自然正确。是否启用由模块的 `go` 指令决定：`go.mod` 写 `go 1.22` 或更高走新语义，写 `go 1.21` 及更早仍按旧语义编译。这正是同一份源码在不同项目里表现不同的原因。

| 对比项 | Go 1.21 及更早 | Go 1.22 及之后 |
| --- | --- | --- |
| 循环变量 | 整轮共用一个 | 每轮各建一个 |
| 闭包捕获 | 全读到最后一轮的值 | 各读到本轮的值 |
| 取址 | 所有指针相同 | 每轮地址不同 |
| 生效依据 | `go.mod` 的 `go` 指令 | 同左，需显式升级 |
| 兼容写法 | 循环体内 `v := v` | 仍可写，非必需 |

<Callout type="warn" title="易错点" text="维护老项目先读 go 指令再判断；升级时只换工具链、不改 go 指令，语义不会变。" />

指针切片容易误判：`range` 拷的是指针值，副本仍指向同一对象，通过它改字段会生效，与「值切片改不动」并不矛盾。map 遍历则完全不保证顺序，语言有意随机化起点；遍历时删除当前键是安全的，新增的键这一轮可能被扫到也可能不被扫到。

```go
package main

import (
	"fmt"
	"slices"
)

func main() {
	type User struct{ Score int }
	users := []*User{{1}, {2}, {3}}
	for _, u := range users {
		u.Score += 10 // 副本仍指向同一对象
	}
	for _, u := range users {
		fmt.Print(u.Score, " ")
	}
	fmt.Println()

	// 要稳定顺序：取键、排序、再按序访问
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys) // Go 1.21 起提供
	for _, k := range keys {
		fmt.Printf("%s=%d ", k, m[k])
	}
	fmt.Println()

	for k := range m { // 遍历中删除是安全的
		if k == "b" {
			delete(m, k)
		}
	}
	fmt.Println("删除后 len =", len(m))
}
```

<OutputBox title="运行结果" output="11 12 13 \na=1 b=2 c=3 \n删除后 len = 2" />

## 4. 工程实践

排查这类 bug 有固定路径：先看 `go.mod` 里 `go` 指令的版本，再确认闭包是否捕获了循环变量，最后检查是否把 `&v` 或 `v` 存进了长生命周期容器。即使已升级到 1.22，显式写 `v := v` 仍是低成本的兼容写法，能让代码在老模块里同样正确。

<Compare title="循环变量交给 goroutine" :panels='[{"label":"写法 A","code":"for _, s := range items {\n    go func() {\n        use(s)\n    }()\n}","note":"旧语义下闭包读到同一个 s"},{"label":"写法 B","code":"for _, s := range items {\n    s := s\n    go func() {\n        use(s)\n    }()\n}","note":"局部副本，各版本都正确"}]' />

<Callout type="tip" title="稳定输出靠排序" text="map 遍历顺序不保证；日志、快照、签名先取键排序，别依赖 range 顺序。" />

## 动手练一练

<Exercise title="修好闭包捕获" task="循环把 i 存进函数切片，结束后调用会得到三个相同的值。不升级 Go 版本，让结果依次为 0、1、2。" :hints='["整轮共用一个循环变量","在循环体内声明同名局部变量"]' solution="var fs []func()\nfor _, i := range []int{0, 1, 2} {\n    i := i\n    fs = append(fs, func() { fmt.Println(i) })\n}\nfor _, g := range fs {\n    g()\n}" explain="i := i 建立独立副本，输出 0、1、2。" />

## 随堂检测

<Quiz no="15-1" q="关于 Go 1.22 循环变量变化，正确的是？" :options='["只影响 range","由 go.mod 的 go 指令决定","闭包改为按值捕获","移除 map 遍历"]' answer="1" explain="1.22 对 for 与 range 同时改为每轮新变量。" />

<Predict no="15-2" q="go.mod 写 go 1.21，把三轮 range 的循环变量地址存进切片再解引用，会打印什么？" :options='["4 5 6","6 6 6","三个不同地址","编译失败"]' answer="1" output="6 6 6" explain="旧语义整轮共用一个变量，三个指针都指向最后的值。" />

## 一页纸速记

<KnowledgeCard :points='["range 给副本，回写用下标","闭包捕获变量而非值","Go 1.22 起每轮新建变量","go 指令决定语义"]' :pitfalls='["把 &v 存进长生命周期容器","升级工具链却没改 go 指令","依赖 map 遍历顺序"]' summary="先确认版本，再看捕获；副本与槽位是两类坑。" />

## 小结与下一讲

- `range` 遍历副本，改元素必须用下标；表达式只在循环前求值一次。
- 闭包捕获变量；Go 1.22 起每轮新建变量，`go.mod` 的 `go` 指令决定走哪套语义。
- 指针切片改的是同一对象，map 遍历顺序不保证，稳定输出要先排序。

序列之外，程序还需要按「键」快速查数据，这就是下一讲的主角。

[下一讲：map：哈希、遍历与 Set 技巧](/basics/maps)
