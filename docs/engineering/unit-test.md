---
title: 第 52 讲 · 单元测试与表驱动测试
description: 用 testing 包写出稳定可读的表驱动测试，掌握 t.Run 子测试、t.Helper、断言库取舍与可测试性设计
---

<StageHeader stage="L5 · 工程化与质量" level="★★★☆☆" time="40 分钟" goal="写出稳定、可读、可维护的测试" prereq="第 51 讲 · 代码规范、gofmt、vet 与 golangci-lint" />

# 第 52 讲 · 单元测试与表驱动测试

> 写测试最容易掉进两个坑：一是每个分支复制一整段断言，改一处要改十处；二是函数里偷偷读时间、连数据库，测试只能靠碰运气。Go 的标准库 `testing` 很朴素，但配合表驱动和一点设计上的克制，就能得到既快又稳的反馈。这一讲的重点不是记住多少断言函数，而是让代码本身变得可测。

## 这一讲你会得到什么
<CheckList id="goal-52" title="本讲目标" :items='["用 testing 包写出可独立运行的 _test.go","把分支整理成表驱动测试并用 t.Run 拆成子测试","用 t.Helper 精准定位失败，判断要不要引入断言库","通过参数化时间与依赖提升可测试性"]' />

## 1. 先建立直觉

把测试想成工厂的质检台：单测是每台零件出厂前的快速量测，量大、便宜、一坏就知道是哪颗螺丝；集成测试是把几个零件装起来试运转；端到端测试是整机跑一遍真实场景。越往上层越贵、越慢、越难定位，所以底座必须最厚。表驱动测试就像一张印好的检查清单：每行写清输入和期望，新增一个边界情况只需加一行，而不是再复制一遍整个工序。

<ConceptDiagram type="test-pyramid" title="测试金字塔：把力气花在底座" />

## 2. 语法与规则

第一块代码是一份完整的 `_test.go`。为了让单个文件就能 `go test`，被测函数一并写在文件里；真实项目里它属于同包的业务文件。

```go
// price_test.go —— 单独一个文件即可 go test 通过
package price

import "testing"

// 被测函数：按数量计算折后总价（示例起见与测试同文件）
func Total(unit, count int, member bool) int {
	sum := unit * count
	if member {
		sum = sum * 9 / 10
	}
	return sum
}

func TestTotal(t *testing.T) {
	cases := []struct {
		name   string
		unit   int
		count  int
		member bool
		want   int
	}{
		{"普通用户无折扣", 100, 2, false, 200},
		{"会员九折", 100, 2, true, 180},
		{"零数量", 100, 0, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Total(c.unit, c.count, c.member)
			assertEqual(t, got, c.want)
		})
	}
}

// t.Helper 让失败信息指向调用它的那一行，而不是辅助函数内部
func assertEqual(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}
```

<OutputBox title="go test -v 运行结果" output="=== RUN   TestTotal\n=== RUN   TestTotal/普通用户无折扣\n=== RUN   TestTotal/会员九折\n=== RUN   TestTotal/零数量\n--- PASS: TestTotal (0.00s)\n    --- PASS: TestTotal/普通用户无折扣 (0.00s)\n    --- PASS: TestTotal/会员九折 (0.00s)\n    --- PASS: TestTotal/零数量 (0.00s)\nPASS\nok      price   0.001s" note="子测试名字来自 t.Run 的第一个参数，失败时能直接定位到具体用例。" />

第二块代码讲可测试性设计：别在函数里偷偷读全局状态，把时间和输入源变成参数，结果就完全可预测。

```go
// 可测试性设计：让函数只依赖参数，不偷偷读全局状态
package main

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Greet 从任意 io.Reader 读名字，时间由调用方传入，结果完全可预测。
func Greet(r io.Reader, now time.Time) (string, error) {
	name, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("读取姓名: %w", err)
	}
	hour := now.Hour()
	return fmt.Sprintf("%s，现在 %d 点", strings.TrimSpace(string(name)), hour), nil
}

func main() {
	t := time.Date(2024, 1, 2, 9, 0, 0, 0, time.UTC)
	s, err := Greet(strings.NewReader("ada"), t)
	if err != nil {
		fmt.Println("失败:", err)
		return
	}
	fmt.Println(s)
}
```

```text
ada，现在 9 点
```

第三块代码补上版本差异：在子测试里用 `t.Parallel` 时，Go 1.21 必须显式复制循环变量，Go 1.22 起循环变量每轮新建才不必写这一行。

```go
// split_test.go —— 单个文件即可 go test 通过
package split

import (
	"strings"
	"testing"
)

// Split 是被测函数（与测试同文件，保证单文件可 go test）
func Split(s, sep string) []string {
	return strings.Split(s, sep)
}

func TestSplit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		sep  string
		want int
	}{
		{"逗号分隔", "a,b,c", ",", 3},
		{"无分隔符", "abc", ",", 1},
		{"空串", "", ",", 1},
	}
	for _, c := range cases {
		c := c // Go 1.22 起可省；Go 1.21 必须复制
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := Split(c.in, c.sep)
			if len(got) != c.want {
				t.Fatalf("段数 got %d, want %d", len(got), c.want)
			}
		})
	}
}
```

<LineByLine title="表驱动测试的最小骨架" code="cases := []struct{ a, b, want int }{\n    {1, 2, 3},\n    {0, 0, 0},\n}\nfor _, c := range cases {\n    if Add(c.a, c.b) != c.want {\n        t.Errorf(...)\n    }\n}" :notes='["把输入与期望收进同一张表","第一组：1 加 2 期望 3","第二组：边界 0 加 0 期望 0","表定义结束","逐行遍历，c 是当次用例","直接比较，不引入额外断言库","失败时报告具体数值","条件闭合","循环结束"]' />

## 3. 深入一步

断言库不是必需品，选择依据是团队规模与失败信息的可读性：

| 方案 | 额外依赖 | 失败信息 | 适合 |
| --- | --- | --- | --- |
| 标准库 t.Errorf | 无 | 自己写清楚 got 与 want | 绝大多数项目 |
| testify/assert | 第三方 | 语义化、自动带上下文 | 团队已统一约定 |
| google/go-cmp | 第三方 | 结构体逐字段差异清晰 | 复杂结构或切片比较 |

还有个取舍值得记下来：子测试要不要 `t.Parallel` 取决于用例之间是否完全独立。共享临时目录、写同一个文件或改包级变量的用例并行起来会互相干扰，此时宁可不并行，也不要为了几毫秒引入偶发失败。表驱动已经让新增用例很便宜，稳定性比速度更重要。

<Term word="t.Helper" text="把当前函数标记为辅助函数，测试失败时报告的是调用它的那一行，而不是辅助函数内部。" />

<Callout type="warn" title="易错点：循环变量与并行" text="Go 1.21 及更早版本里，range 循环变量只有一份；子测试若调用 t.Parallel，闭包捕获的会是同一个变量，导致所有子测试跑最后一组数据。要么显式 c := c，要么把 go.mod 的 go 版本升到 1.22 以上。" />

## 4. 工程实践

真实项目里测试变慢、变脆，几乎都不是断言写得不好，而是被测函数抓了不该抓的东西：`time.Now()`、真实数据库、真实 HTTP。判断标准很直接——**这个依赖在测试里能不能被一个固定值顶替**。

测试的命名同样是维护成本的一部分。用例名写业务情形而不是编号，失败时用 `go test -run` 才能直接定位；错误信息里同时给出 got 与 want，而不是只报一句不相等，能省掉一半的复现时间。表驱动把这些约定固化成模板，新人照着加一行即可，不必重新发明断言方式。

<Compare title="不可测与可测的差别" :panels='[{"label":"写法 A · 抓全局状态","code":"func Total() int {\n    return time.Now().Year() * queryDB()\n}\n// 依赖当前时间与真实库，结果无法复现","note":"每次跑都可能不同，只能靠 sleep 或连真库，反馈慢且偶发失败"},{"label":"写法 B · 依赖从参数进","code":"func Total(now time.Time, q Querier) int {\n    return now.Year() * q.Amount()\n}\n// 时间与查询都由调用方控制","note":"传入固定时间与替身即可稳定复现，毫秒级跑完"}]' />

几个反模式值得点名：为了「覆盖率好看」给 getter 写一堆断言，测不到任何行为；测试里 `time.Sleep` 等异步结果，把不确定性带进 CI；用例之间共享可变全局变量，单独跑能过、全量跑就挂。表驱动的价值恰恰是把输入、期望、用例名排成一张可审阅的清单，而不是把三段重复的断言代码复制三遍。

<Callout type="tip" title="一条可执行的检查" text="在 CI 里固定跑 go test -race ./...，并把 -count=1 加上，避免测试结果被缓存掩盖。表驱动用例名用中文描述业务情形，失败信息才能一眼看懂。" />

## 动手练一练

<Exercise title="为 Max 写一张用例表" task="函数 Max(a, b int) int 返回较大值。请写一份表驱动测试，至少覆盖「一大一小」与「相等」两种情形，用 t.Run 命名子测试。" :hints='["用一个匿名结构体切片装 a、b、want","循环里用 t.Run 传入用例名","用 t.Errorf 打印 got 与 want"]' solution="func TestMax(t *testing.T) {\n    cases := []struct{ name string; a, b, want int }{\n        {name: `一大一小`, a: 1, b: 2, want: 2},\n        {name: `相等`, a: 5, b: 5, want: 5},\n    }\n    for _, c := range cases {\n        t.Run(c.name, func(t *testing.T) {\n            if got := Max(c.a, c.b); got != c.want {\n                t.Errorf(...)\n            }\n        })\n    }\n}" explain="用例表把输入与期望并排写清楚，新增边界只需加一行；t.Run 让每个情形独立报告。" />

## 随堂检测

<Quiz no="52-1" q="在断言辅助函数里调用 t.Helper() 的主要作用是什么？" :options='["让测试并行执行","让失败信息指向调用辅助函数的那一行","自动重试失败的用例","把用例标记为跳过"]' answer="1" explain="t.Helper 把自身从失败堆栈里隐藏，报告定位到真正出错的调用处，排查成本大幅下降。" />

<Predict no="52-2" q="执行 go test -run TestTotal/会员 -v 时，哪些用例会被执行？" :options='["三个子测试全部执行","只执行 TestTotal/会员九折","只执行普通用户无折扣","一个都不执行，因为 -run 不支持斜杠"]' answer="1" output="=== RUN   TestTotal\n=== RUN   TestTotal/会员九折\n--- PASS: TestTotal (0.00s)\n    --- PASS: TestTotal/会员九折 (0.00s)" explain="-run 的斜杠用来匹配子测试名，父测试仍会运行，但只进入名字匹配的子测试。" />

## 一页纸速记

<KnowledgeCard :points='["表驱动：输入、期望、用例名放进一张结构体切片","t.Run 拆子测试，失败直接定位到具体情形","t.Helper 让辅助函数不污染失败位置","时间、随机数、外部依赖改成参数传入，结果才可复现"]' :pitfalls='["Go 1.21 子测试并行时忘了复制循环变量","测试里 sleep 等异步、连真实数据库","断言库层层引入，却只测了 getter"]' summary="好测试的前提是好设计：把依赖收进参数，剩下的交给一张可审阅的用例表。" />

## 小结与下一讲

- `testing` 包加表驱动，就能覆盖绝大多数分支，且新增用例只需加一行。
- `t.Run` 与 `t.Helper` 分别负责定位失败位置和保持失败信息干净。
- 可测试性来自设计：时间与外部状态参数化，断言库按团队需要取舍。

[下一讲：Mock：gomock / sqlmock / httptest](/engineering/mock-test)
