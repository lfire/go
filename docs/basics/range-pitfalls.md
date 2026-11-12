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

为什么 1.22 之前所有闭包读到同一个值？因为闭包捕获的是「变量」这个存储单元，而不是某一刻的值。被闭包引用的局部变量会从栈搬到堆上（这就是逃逸），整个循环只分配了一个这样的单元，每轮只是把新值写进去；闭包记住的是单元的地址，等循环跑完再调用，读到的自然是最后一轮留下的值。用 `go build -gcflags=-m` 编译旧语义的代码，输出里能看到循环变量 `escapes to heap`；1.22 起改为每轮在循环体内新建变量，等价于编译器替你自动写了 `v := v`，于是每个闭包各指向一个独立单元。`range` 被降级成普通循环的代码在 `cmd/compile/internal/walk/range.go`——这里只需知道路径，真读源码是后续阶段的事。

决定是否启用新语义的是 `go.mod` 里的 `go` 指令，而不是你机器上装了哪个版本的 Go。这条最容易踩：把构建机从 1.21 升到 1.23，但模块仍写 `go 1.21`，编译器就继续按旧语义处理，行为纹丝不动。验证顺序是先看 `go version` 再看 `go.mod`，必要时显式把指令改成 `go 1.22` 并 `go mod tidy`。想在同一台机器上对比，可以用 `GOEXPERIMENT=loopvar go run main.go` 在新旧过渡期预览。把循环变量交给 goroutine 的旧代码还藏着数据竞争：主循环写、goroutine 读同一个变量，`go test -race` 会明确报出 `DATA RACE`，这也是升级前最值得跑的一条命令。

生产里真实的翻车现场有两个。其一是把 `&v` 存进任务队列，队列异步消费时全部指向最后一条记录，表现为「批量任务只处理了同一条数据」，用 `-race` 一跑就现形。其二是拿循环变量当分片下标启动 goroutine，结果所有协程挤到同一片，负载全压在一个核上——`top` 与 pprof 里会看到明显的倾斜。更隐蔽的一类是 map 遍历顺序被写进签名，偶发验签失败；先取键排序往往当场解决。

<FlowTimeline title="排查闭包捕获的四步" :steps='[{"title":"查版本","desc":"读 go.mod 的 go 指令","tone":"blue"},{"title":"找捕获","desc":"闭包是否用了循环变量","tone":"orange"},{"title":"看存址","desc":"&v 是否进了长生命周期容器","tone":"green"},{"title":"加副本","desc":"v := v 兼容所有版本","tone":"purple"}]' direction="horizontal" />

下面用两段代码把「版本差异」和「nil 边界」变成能亲手跑的实验。第一段把循环变量直接交给 goroutine，1.22 起各写各的下标，旧版本则会漏写并触发竞态。

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	nums := []int{1, 2, 3}
	var wg sync.WaitGroup
	out := make([]int, len(nums))
	for i, v := range nums {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Go 1.22 起 i、v 每轮新建，各 goroutine 互不干扰；
			// 1.21 及更早共用一个变量，会读到最后一轮且构成数据竞争
			out[i] = v * 10
		}()
	}
	wg.Wait()
	fmt.Println("结果 =", out)
}
```

<OutputBox title="Go 1.22 及之后的运行结果" output="结果 = [10 20 30]" />

第二段换成指针切片，演示最容易漏掉的 nil 边界：切片里可能混入 nil 指针，批量校验时要在循环体内判空，并且「找不到/非法」要当成明确结果返回而不是悄悄跳过。

```go
package main

import "fmt"

type User struct {
	Name  string
	Score int
}

// validate 检查指针切片里有没有 nil 元素或负分，发现即整体返回错误。
func validate(users []*User) error {
	for i, u := range users {
		if u == nil { // 真边界：指针切片可能混入 nil
			return fmt.Errorf("第 %d 个元素是 nil", i)
		}
		if u.Score < 0 {
			return fmt.Errorf("用户 %s 分数非法", u.Name)
		}
	}
	return nil
}

func main() {
	users := []*User{{Name: "a", Score: 90}, {Name: "b", Score: 80}}
	fmt.Println("全部合法 =", validate(users) == nil)

	users = append(users, nil) // 混入一个 nil 指针
	fmt.Println("含 nil 时报错 =", validate(users) != nil)

	fmt.Println("nil 切片也合法 =", validate(nil) == nil)
}
```

<OutputBox title="运行结果" output="全部合法 = true\n含 nil 时报错 = true\nnil 切片也合法 = true" />

| 现象 | 根因 | 定位或修法 |
| --- | --- | --- |
| 一串结果全相同 | 闭包捕获了共享的循环变量（≤1.21） | `go test -race` 查竞态；改用 `v := v` |
| 改了元素却没生效 | `range` 给的是值副本 | 用下标 `s[i]` 回写，或改存指针切片 |
| 输出顺序每次不同 | map 遍历起点随机 | 取键排序后再访问 |
| 升级 Go 后行为没变 | 只换工具链，`go.mod` 的 `go` 指令没升 | 查 `go version` 与 `go.mod`，显式改 `go 1.22` |
| 遍历指针切片时 panic | 切片里混入了 nil 指针 | 循环体先判 `u == nil` |

<Predict no="15-3" q="go.mod 写 go 1.21，把 range 的 i、v 直接交给 goroutine 写 out[i]，wg.Wait 之后最可能出现什么？" :options='["稳定得到 [10 20 30]","多个下标被漏写，且 go test -race 报数据竞争","编译失败","固定输出 [30 30 30]"]' answer="1" output="多个下标被漏写，且 go test -race 会报 DATA RACE" explain="旧语义下 i、v 共用一个变量并被主循环与 goroutine 并发读写，既是数据竞争也会让多个协程写到同一处；1.22 起每轮新建变量才稳定得到 [10 20 30]。" />

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
