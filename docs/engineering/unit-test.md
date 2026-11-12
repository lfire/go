---
title: 第 052 讲 · 单元测试与表驱动测试
description: 用 testing 包写出稳定可读的表驱动测试，掌握 t.Run 子测试、t.Helper、断言库取舍与可测试性设计
---

<StageHeader stage="L5 · 工程化与质量" level="★★★☆☆" time="40 分钟" goal="写出稳定、可读、可维护的测试" prereq="第 051 讲 · 代码规范、gofmt、vet 与 golangci-lint" />

# 第 052 讲 · 单元测试与表驱动测试

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

### 表驱动、t.Run、t.Helper 为什么长这样

这几个写法不是约定俗成，而是被 Go 的语言能力推出来的。Go 没有专门的测试 DSL，`go test` 做的事很简单：把包里的 `TestXxx(t *testing.T)` 函数收集起来，生成一个临时的 main 包逐个调用。测试因此就是同包的普通 Go 代码，用同一套编译器与类型检查——这解释了为什么断言库是可选的，`t.Errorf` 加一句清楚的 `got / want` 就够用，也解释了为什么测试文件能直接访问同包的非导出函数。

`t.Run` 解决的是「表格第几行挂了」这个定位问题。没有子测试时，一次循环里任何一组数据失败都只报同一个 `TestTotal failed`，你得自己把输入打进错误信息；有了子测试，每个用例有独立名字，`go test -run` 能精确重跑某一个，失败输出直接带路径。`t.Helper` 则补上另一半：`t.Errorf` 默认报告调用它的那一行，一旦把断言封装进辅助函数，所有用例的失败都会指向辅助函数内部同一行，等于失去定位；标记 `t.Helper()` 后该帧被隐藏，报告回到真正的调用处。表驱动之所以成为 Go 的惯例，是因为语言没有原生参数化测试语法，用「匿名结构体切片 + for + t.Run」是能表达「输入 / 期望 / 用例名」三元组的最轻结构，且新增边界只加一行，代码 diff 天然可审阅。

| 写法 | 新增一个边界用例 | 失败定位 | 重构时的代价 |
| --- | --- | --- | --- |
| 复制粘贴断言 | 再抄一整段，容易漏改 | 只报顶层测试失败，不知哪组数据 | 改一处要同步十处 |
| 参数化测试框架 | 加注解或参数 | 依赖框架的报告格式 | 多一层 DSL 与第三方依赖 |
| 表驱动 + t.Run | 表里加一行 | 直接报子测试名 | 断言只在一处，改动最小 |

### 可验证实验：覆盖率与竞态一起看

下面两条命令是 CI 里最值得固定下来的组合，逐字段读懂输出比记住命令更重要。

```bash
go test -race -count=1 -coverprofile=cover.out ./...
go tool cover -func=cover.out
```

```text
ok  	example.com/shop/price	1.023s	coverage: 83.3% of statements
example.com/shop/price/price.go:8:	Total		83.3%
example.com/shop/price/price.go:20:	Discounted	100.0%
total:					(statements)	87.5%
```

逐字段看第一条命令的输出：`ok` 表示通过，紧接着是包路径、耗时、`coverage: 83.3% of statements` 表示被执行到的语句占比；`-race` 会让耗时明显变长，因为它给每次内存访问加了检测。`go tool cover -func` 则按函数展开，每行的四段依次是「文件:行号」「函数名」「该函数语句覆盖率」，最后的 `total` 是整包口径。两个开关各有不可替代的作用：`-count=1` 禁用测试结果缓存，否则只改了被测实现、测试文件没变时，`go test` 可能直接复用上次的 `ok`，让回归悄悄溜过；`-race` 把并发用例里的数据竞争在测试期就报出来，而不是留到线上偶发（呼应第 049 讲）。覆盖率数字只用来找「一行都没跑过的盲区」，不能当成质量分数——原因见下一节的误解。

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

### go test 把你的测试函数变成了什么：_testmain.go 与用例注册表

上一节说「`go test` 收集 `TestXxx` 生成一个临时 main 包」，这句话可以追到实现。`go test` 并不直接调用你写的测试函数：它先由 `cmd/go/internal/load/test.go` 扫描包里的 `TestXxx(*testing.T)`、`BenchmarkXxx(*testing.B)`、`FuzzXxx(*testing.F)`、`ExampleXxx`，在临时工作目录里生成一个 `package main` 的 `_testmain.go`，把这些函数装进四张注册表，再交给 `testing` 包调度。这份生成物是编译期产物，因此测试函数签名写错会表现为**编译错误**而不是运行时报错；也正因为测试二进制本身就是普通可执行文件，`go test -c` 能把它单独编成 `pkg.test`。

由此能推出几条实践结论，每条都能反证：`-run` 之所以能用斜杠精确匹配子测试，是因为它先在这张静态注册表上按名字过滤，子测试再由 `t.Run` 运行时展开（所以 `-run` 不能凭空造出表里没有的用例）；`TestMain(m *testing.M)` 能接管整个流程，是因为生成物默认调 `m.Run()`，一旦包里定义了 `TestMain`，生成物就改成调用它，你在里面忘了调 `m.Run()` 所有用例都不会执行；同包测试能直接访问非导出函数，是因为生成物以 `_test "被测包"` 的别名把被测包 import 进来，而 `xxx_test` 外部测试包会另占一个导入项、只能看到导出标识符。

可复现实验（Go 1.23.8 实测）。`-work` 让 `go test` 保留临时目录，直接把生成物读出来：

```bash
go test -work -run TestAdd -v ./...
# 记下输出里的 WORK=/var/folders/.../go-buildNNN
cat "$WORK/b001/_testmain.go"
```

```text
// Code generated by 'go test'. DO NOT EDIT.

package main

import (
	"os"
	"testing"
	"testing/internal/testdeps"

	_test "example.com/tm"
)

var tests = []testing.InternalTest{
	{"TestAdd", _test.TestAdd},
}

var benchmarks = []testing.InternalBenchmark{
	{"BenchmarkAdd", _test.BenchmarkAdd},
}

var fuzzTargets = []testing.InternalFuzzTarget{}
var examples = []testing.InternalExample{}

func init() {
	testdeps.ImportPath = "example.com/tm"
}

func main() {
	m := testing.MainStart(testdeps.TestDeps{}, tests, benchmarks, fuzzTargets, examples)
	os.Exit(m.Run())
}
```

逐字段看：首行 `Code generated ... DO NOT EDIT` 说明它是工具生成的临时源码，不入库、也不该手改；`package main` 证明测试二进制就是一个独立可执行程序；`_test "example.com/tm"` 是被测包的别名导入，同包测试被合并进这个包，外部测试包则是另一个导入项；`var tests` 是核心注册表，`TestAdd` 这个名字与函数值成对存放，`-run` 正是在这张表上过滤；`benchmarks` / `fuzzTargets` / `examples` 是另外三张表，分别对应 `-bench`、`-fuzz` 与 `Example`（呼应第 054 讲）；`testdeps.ImportPath` 让 `testing` 知道当前包路径，用于覆盖率与输出；最后 `testing.MainStart(...)` 把四张表交给 `M`，`os.Exit(m.Run())` 启动调度。整个过程没有解释器、没有反射查找测试函数，全靠编译期生成的静态表——这就是「测试是同包普通 Go 代码」的底层依据。

**边界与失效条件**：`_testmain.go` 每次构建的 WORK 路径都不同（`go test -work` 才保留），不能当源码引用；它只登记函数，不登记子测试，所以 `-run 'TestA/子用例'` 的匹配发生在 `t.Run` 运行时；注册表是编译期快照，测试文件里通过 `init` 动态注册的函数不会出现。`go test -c` 产出的 `pkg.test` 不含覆盖率插桩，除非同时给 `-cover`（插桩由 `cmd/cover` 改写源码，见第 054 讲）。机制能证明「测试被正确编译与登记」，证明不了断言是否正确——那是覆盖率与误解一节要澄清的。

**版本前提**：本实验在 Go 1.23.8（darwin/arm64）实测。`testing.MainStart` 的签名随版本演进——`examples` 参数自 Go 1.7 起加入、`fuzzTargets` 自 Go 1.18（fuzz 落地）起加入——所以不同 Go 版本生成的 `_testmain.go` 字段数不同；`go test -work`、`-c`、`-run` 的行为长期稳定。

### `M.Run` 怎么放行并行子测试：`testContext` 信号量与 PAUSE / CONT 的时序

前面 `_testmain.go` 那一节讲到：`testing.MainStart` 把四张注册表交给 `M`，`m.Run()` 负责调度。但「`t.Parallel()` 之后子测试到底去哪了」还没追到实现——这直接决定了 `-parallel` 为什么能限流，以及为什么并行子测试看起来「先全部停住、再一批批放行」。

机制在 `src/testing/testing.go`。`runTests`（约 2139 行）为每个顶层测试建一个 `testContext`：`ctx := newTestContext(*parallel, ...)`。注意传进去的第一个参数就是 `-parallel` 的值（默认 `GOMAXPROCS`），它被存成 `maxParallel`；同一函数里 `running: 1` 的初值是给「主顺序测试」预留的名额（`newTestContext`，1800 行）。整个限流是一个手写信号量，只有两个方法（约 1809、1821 行）：`waitParallel` 先上锁，若 `running < maxParallel` 就把 `running` 加一放行，否则把 `numWaiting` 加一后阻塞在无缓冲 channel `<-c.startParallel` 上；`release` 反过来，若 `numWaiting == 0` 就把 `running` 减一，否则把 `numWaiting` 减一并 `c.startParallel <- true` **精确唤醒一个等待者**。

关键在于它**不是**用带缓冲 channel 当令牌桶，而是 `running` 计数加 `startParallel` 无缓冲 channel：`running` 记录当前占用名额数，`numWaiting` 记录排队数。于是「同时在跑的并行测试数」恒满足 `running <= maxParallel`，这是个不变量——`release` 唤醒一个等待者时并不把 `running` 加一，因为名额是从结束者手里直接「交接」过去的，计数始终守恒。

`t.Parallel()`（1443 行）把这个不变量和两道栅栏串起来：先把自己登记进 `t.parent.sub`，打印 `=== PAUSE`，用 `t.signal <- true` 通知 `t.Run`「我暂时让出」，然后 `<-t.parent.barrier` 等父测试跑完，再 `t.context.waitParallel()` 申请名额，拿到后打印 `=== CONT` 并把 `t.start` 重置——**这就是「PAUSE 的耗时不计入测试时长」的实现**：第 1461 行先把已耗时间累加进 `duration`，CONT 之后重新计时。父测试一侧（`tRunner` 约 1640 行）在顺序体返回后，先 `t.context.release()` 归还自己那个名额，再 `close(t.barrier)` 一次性唤醒所有并行子测试，最后逐个 `<-sub.signal` 等它们结束。所以父子之间形成三段：顺序体执行 → 父测试归还名额并打开栅栏 → 子测试按信号量分批拿名额执行。

这套时序可以用 `-json` 的事件时间戳直接量出来。实验（Go 1.23.8，darwin/arm64；4 个子测试各 `t.Parallel()` 后 `time.Sleep(30*time.Millisecond)`）：

```go
package p

import (
	"testing"
	"time"
)

func TestParallel(t *testing.T) {
	for _, name := range []string{"a", "b", "c", "d"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			time.Sleep(30 * time.Millisecond)
		})
	}
}
```

```bash
go test -json -v -parallel=2 -count=1 -run TestParallel . | grep -E '"Action":"(pause|cont|pass)"'
```

```text
（把每条事件的 Time 减去第一个 cont 的时刻，4 个子测试的 cont 时刻如下）
-parallel=1 :  a +0.0ms   b +31.0ms   d +62.1ms   c +92.4ms     全部 pass 约 +123ms
-parallel=2 :  a +0.0ms   c +0.0ms  |  d +30.7ms   b +30.8ms     全部 pass 约 +61ms
-parallel=4 :  a +0.0ms   b +0.0ms   d +0.0ms   c +0.0ms         全部 pass 约 +31ms
```

逐字段：`pause` 事件（对应 `=== PAUSE`）在四种设置下都先于所有 `cont` 出现——父测试的顺序体把 4 个 `t.Run` 全部触发、4 个子测试各自登记并 `PAUSE` 之后，`close(t.barrier)` 才发生，这解释了「先全部停住、再放行」。`-parallel=1` 时四个 `cont` 相隔约 31ms 依次出现，因为 `running` 恒为 1，每个子测试跑完（sleep 30ms）`release` 才唤醒下一个；`-parallel=2` 时 `a`、`c` 同批 `cont`（+0ms），跑完释放两个名额后 `d`、`b` 在 +30.7ms 同批 `cont`；`-parallel=4` 时 4 个名额一次给满，全部 +0ms。三档总时长分别约 123ms、61ms、31ms。

由此得到一个可代入的公式：设 `N` 个并行子测试、每个耗时 `T`、`-parallel=P`，则批次数 `B = ⌈N / P⌉`，理想总时长 `≈ B × T`，且同一时刻 `cont` 未 `pass` 的子测试数不超过 `P`。代入实测：N=4、T=30ms，P=1 → B=4 → 120ms（实测 123ms），P=2 → B=2 → 60ms（实测 61ms），P=4 → B=1 → 30ms（实测 31ms），误差来自调度与计时开销。这条公式是可证伪的：若把 `t.Parallel()` 换成不并行，总时长恒为 `N × T = 120ms`、与 P 无关；若 `-parallel` 真能「开更多 goroutine」，P=1 时就不该出现 31ms 的间隔。

从机制推出三条实践结论，每条都能反证：其一，`-parallel` 限的是**并行测试数**而非 goroutine 数，测试体里自己起 100 个 goroutine 不受它约束，`M.Run` 只看 `t.Parallel` 登记进 `sub` 的那些；其二，`PAUSE` 期间不计时（1461 行重置 `t.start`），所以「并行测试都很慢」的直觉常是错的——输出里的 elapsed 只算真正占用名额跑的时间，排队等待被排除；其三，`t.Parallel` 后不能再 `t.Setenv`（1448 行直接 panic），因为环境变量是进程级的，并行子测试共享进程，谁改都会串扰——源码的 `Setenv` 还专门沿 `parent` 链检查有没有并行祖先（1516 行），这是「共享可变状态别并行」在类型系统层面的硬约束，呼应本讲误解二。

**`TestMain` 与覆盖率插桩的交互**：第二版讲过「定义 `TestMain` 后，`_testmain.go` 改成调用它，忘了 `m.Run()` 则所有用例不执行」。补一层覆盖率并给出可复现对照：`go test -cover` 的计数器由 `cmd/cover` 插桩进源码，`-coverprofile` 文件的 `mode: set` 表头由 `go` 命令预写，而**各基本块的计数由 `m.Run()` 收尾时填充**。实测三种 `TestMain`（Go 1.23.8，被测包里 `Add` 测到、`Sub` 没测到，理论 50%）：

```text
A  func TestMain(m *testing.M){ os.Exit(m.Run()) }  → ok … coverage: 50.0%   cov.out total 50.0%
B  func TestMain(m *testing.M){ os.Exit(0) }        → ok …（无 coverage 行） cov.out 仅 mode: set，total 0.0%
C  func TestMain(m *testing.M){ m.Run() }           → ok … coverage: 50.0%   cov.out total 50.0%
```

逐字段：B 跳过 `m.Run()`，测试一个都没跑，但 `go test` 仍打印 `ok`、退出码 0——**这个 `ok` 是假象**，没有任何用例执行；`-coverprofile` 只剩表头、覆盖率为 0.0%，若 CI 只拿「`go test` 退出码」当门禁，这次空跑会被当成通过。C 证明写盘发生在 `m.Run()` 内部而非 `os.Exit` 之后，所以「不取返回码」不影响覆盖率；A、C 都拿到 50.0%，是 `Add` 那块置位、`Sub` 那块为 0 的正确结果。判断标准因此很具体：覆盖率数据由 `m.Run()` 收尾写出，`TestMain` 只要不调用 `m.Run()`，profile 就是空表；把 CI 门禁写成「检查 `coverage:` 行或 `go tool cover -func` 的 total 是否达标」，比只看退出码更能挡住这类空跑。这也解释了为什么 `go test -c` 编出的 `pkg.test` 不带 `-cover` 时没有计数器（计数器是编译期插桩，见第 054 讲）。

**边界与失效条件**：`-parallel` 默认 `GOMAXPROCS`，CI 的 CPU 核数与本地不同会直接改变并行批次与时序，「本地稳定、CI 偶发」有时正是它，而不是代码有竞态；`close(t.barrier)` 是一次性放行，父测试体里若还有依赖子测试结果的逻辑，那部分在 `PAUSE` 之后、子测试 `CONT` 之前执行会读到未完成状态；上面公式假设每个子测试耗时相近且彼此独立，某个用例特别慢或持锁时，`release` 精确唤醒一个等待者会让后续批次被它拖住，实际总时长退化为接近最慢用例之和。

**版本前提**：本机 Go 1.23.8（darwin/arm64）实测；`testContext` 的 `running / numWaiting / startParallel` 实现、`t.Parallel` 的三段时序（`signal`、`barrier`、`waitParallel`）自 Go 1.7 引入子测试以来结构稳定，`t.Setenv` 的自 Go 1.17 起；`t.Parallel` 在 fuzz 目标里无效（1451 行 `t.parent.barrier == nil` 时直接返回），这是第 054 讲 fuzz 每个 input 单进程独占的同一个原因。第 052 讲前面 `_testmain.go` 讲「测试被编译成什么」，这里补的是「`m.Run()` 拿到注册表之后怎么按信号量调度」。

### 三条最常见的误解

**误解一：覆盖率 100% 就等于没有 bug。** 反例：函数 `func Div(a, b int) int { return a / b }` 用一组 `Div(4, 2)` 就能拿到 100% 的行覆盖，但 `b = 0` 会直接 panic；更隐蔽的是断言写成 `if got != want { t.Log(...) }`——忘了 `Fail`，覆盖率照样满分，测试却永远不会红。覆盖率只度量「语句是否被执行过」，不度量输入组合、不度量断言是否正确、也不度量错误路径的返回值有没有被检查。正确用法是把它当盲区探测器：`go tool cover -func` 里哪个函数是 0.0%，就先补那一块，边界值再交给第 054 讲的 fuzz 去穷举。

**误解二：`t.Parallel` 加得越多越快，表驱动里无脑加就行。** 反例：几个子测试共写一个临时文件、改同一个包级变量，或复用同一个 `httptest` server，加了并行之后开始偶发失败，而且因为调度交错每次挂的用例都不同，极难复现；Go 1.21 及更早版本还有循环变量捕获问题，所有子测试可能都跑最后一组数据。正确做法：只有用例之间完全独立才并行；共享状态的用例子测试保持串行，稳定性比省下的几毫秒重要得多。

**误解三：引入断言库，测试自然更可靠。** 反例：换上 `testify/assert` 后，团队在 `assert.Equal` 里不再写清 got 与 want 的业务含义，失败时仍要翻日志复现；第三方库本身还成了需要升级的依赖，升级后输出格式变化会连带改一堆断言。标准库加一句写清楚「期望什么、实际什么」的 `t.Errorf`，在绝大多数项目里已经够用；只有比较嵌套结构体、切片且需要逐字段 diff 时，`go-cmp` 的收益才值回引入成本。

## 4. 工程实践

真实项目里测试变慢、变脆，几乎都不是断言写得不好，而是被测函数抓了不该抓的东西：`time.Now()`、真实数据库、真实 HTTP。判断标准很直接——**这个依赖在测试里能不能被一个固定值顶替**。

测试的命名同样是维护成本的一部分。用例名写业务情形而不是编号，失败时用 `go test -run` 才能直接定位；错误信息里同时给出 got 与 want，而不是只报一句不相等，能省掉一半的复现时间。表驱动把这些约定固化成模板，新人照着加一行即可，不必重新发明断言方式。

<Compare title="不可测与可测的差别" :panels='[{"label":"写法 A · 抓全局状态","code":"func Total() int {\n    return time.Now().Year() * queryDB()\n}\n// 依赖当前时间与真实库，结果无法复现","note":"每次跑都可能不同，只能靠 sleep 或连真库，反馈慢且偶发失败"},{"label":"写法 B · 依赖从参数进","code":"func Total(now time.Time, q Querier) int {\n    return now.Year() * q.Amount()\n}\n// 时间与查询都由调用方控制","note":"传入固定时间与替身即可稳定复现，毫秒级跑完"}]' />

几个反模式值得点名：为了「覆盖率好看」给 getter 写一堆断言，测不到任何行为；测试里 `time.Sleep` 等异步结果，把不确定性带进 CI；用例之间共享可变全局变量，单独跑能过、全量跑就挂。表驱动的价值恰恰是把输入、期望、用例名排成一张可审阅的清单，而不是把三段重复的断言代码复制三遍。

<Callout type="tip" title="一条可执行的检查" text="在 CI 里固定跑 go test -race ./...，并把 -count=1 加上，避免测试结果被缓存掩盖。表驱动用例名用中文描述业务情形，失败信息才能一眼看懂。" />

### 一次真实故障：单独跑全绿，全量跑就挂

现象：CI 上 `go test ./...` 偶发失败，失败的用例名每次还不一样；开发本地 `go test -run TestOrder` 却百分百通过。排查路径：

1. 先看失败断言，发现和订单逻辑无关，指向一个被复用的计数器，于是怀疑用例之间共享状态；
2. 用 `grep -R '^var ' ./internal` 找出包级全局 map 与计数器，确认多个测试都在读写它；
3. 加 `go test -count=1 ./...` 复现——不带这个参数时，部分包命中了结果缓存，把不稳定遮住了；
4. 再开 `go test -race ./...`，直接报出 `DATA RACE`：几个带 `t.Parallel` 的子测试同时写那个全局 map；
5. 根因收敛为两句：用例共享可变全局状态，且被并行调度；单独跑没有交错，全量跑才触发。

修复分两步：把共享状态收进每个用例自己构造的局部结构体，让替身与计数器随用例生灭；确需复位的包级资源用 `t.Cleanup` 注册清理，而不是靠人记得在用例末尾写。`t.Cleanup` 为什么存在：它把清理动作绑定到该测试（含子测试）的生命周期末尾，即便中途 `t.Fatal` 提前返回也会执行，比散落在各处的 `defer` 更不容易漏，且子测试结束时立即触发，不会拖到整个父测试结束。

替代做法与代价也清楚了：用 `time.Sleep` 等异步结果，把不确定性直接写进测试，CI 越忙越容易挂；连真库跑，反馈慢且要准备外部状态；用全局状态但强制串行，看似稳定，实则把并发 bug 一起藏了起来。参数化时间与依赖（本讲第二块代码）是唯一同时拿到「快、稳、可复现」的做法，也是第 053 讲 mock 能成立的前提。

### 把测试接进 CI

```yaml
name: test
on: [pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - name: 单测、竞态与覆盖率
        run: |
          go test -race -count=1 -coverprofile=cover.out ./...
          go tool cover -func=cover.out
      - name: 打印整体覆盖率
        run: go tool cover -func=cover.out | tail -1
```

版本锁定是这里的重点：`go-version-file: go.mod` 让 CI 用 `go.mod` 声明的 Go 版本，而不是浮动的 `latest`。Go 1.22 起循环变量每轮新建，直接影响本讲第三块代码里「表驱动 + t.Parallel」要不要写 `c := c`；本地 1.22、CI 1.21 时，同一份测试会在 CI 上跑出完全不同的结果，且很难联想到是工具链版本。`-count=1` 禁用结果缓存，`cache: true` 只缓存依赖下载而不缓存测试结论，`actions/checkout@v4` 与 `setup-go@v5` 钉住大版本，都是为了避免「今天绿明天红」这类与代码无关的漂移（第 060 讲会展开）。

### 与 L10 实战工程结构的呼应

L10 的贯穿线从第一讲就靠这套测试守着：`/projects/kv-1-memory` 为 `Storage` 接口写表驱动用例，`/projects/kv-2-network`、`/projects/kv-3-async` 加网络帧与 WAL 后，用 `-race` 挡住并发回归；`/projects/cli-todo` 的命令解析用一张用例表覆盖参数组合；第 054 讲 fuzz 找到的崩溃输入会回填成表里新的一行，第 053 讲提供的边界替身则让这些用例不必连真库。测试金字塔的底座不是一次性搭完的，而是每加一层能力就补一行用例——这也是为什么第 050 讲的可替换结构值得先花力气做对。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l5-engineering/lesson-052-unit-test`](https://github.com/lfire/go/tree/main/examples/l5-engineering/lesson-052-unit-test)。clone 仓库后进入 `examples/l5-engineering`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="为 Max 写一张用例表" task="函数 Max(a, b int) int 返回较大值。请写一份表驱动测试，至少覆盖「一大一小」与「相等」两种情形，用 t.Run 命名子测试。" :hints='["用一个匿名结构体切片装 a、b、want","循环里用 t.Run 传入用例名","用 t.Errorf 打印 got 与 want"]' solution="func TestMax(t *testing.T) {\n    cases := []struct{ name string; a, b, want int }{\n        {name: `一大一小`, a: 1, b: 2, want: 2},\n        {name: `相等`, a: 5, b: 5, want: 5},\n    }\n    for _, c := range cases {\n        t.Run(c.name, func(t *testing.T) {\n            if got := Max(c.a, c.b); got != c.want {\n                t.Errorf(...)\n            }\n        })\n    }\n}" explain="用例表把输入与期望并排写清楚，新增边界只需加一行；t.Run 让每个情形独立报告。" />

## 随堂检测

<Quiz no="052-1" q="在断言辅助函数里调用 t.Helper() 的主要作用是什么？" :options='["让测试并行执行","让失败信息指向调用辅助函数的那一行","自动重试失败的用例","把用例标记为跳过"]' answer="1" explain="t.Helper 把自身从失败堆栈里隐藏，报告定位到真正出错的调用处，排查成本大幅下降。" />

<Predict no="052-2" q="执行 go test -run TestTotal/会员 -v 时，哪些用例会被执行？" :options='["三个子测试全部执行","只执行 TestTotal/会员九折","只执行普通用户无折扣","一个都不执行，因为 -run 不支持斜杠"]' answer="1" output="=== RUN   TestTotal\n=== RUN   TestTotal/会员九折\n--- PASS: TestTotal (0.00s)\n    --- PASS: TestTotal/会员九折 (0.00s)" explain="-run 的斜杠用来匹配子测试名，父测试仍会运行，但只进入名字匹配的子测试。" />

## 一页纸速记

<KnowledgeCard :points='["表驱动：输入、期望、用例名放进一张结构体切片","t.Run 拆子测试，失败直接定位到具体情形","t.Helper 让辅助函数不污染失败位置","时间、随机数、外部依赖改成参数传入，结果才可复现"]' :pitfalls='["Go 1.21 子测试并行时忘了复制循环变量","测试里 sleep 等异步、连真实数据库","断言库层层引入，却只测了 getter"]' summary="好测试的前提是好设计：把依赖收进参数，剩下的交给一张可审阅的用例表。" />

## 小结与下一讲

- `testing` 包加表驱动，就能覆盖绝大多数分支，且新增用例只需加一行。
- `t.Run` 与 `t.Helper` 分别负责定位失败位置和保持失败信息干净。
- 可测试性来自设计：时间与外部状态参数化，断言库按团队需要取舍。

[下一讲：Mock：gomock / sqlmock / httptest](/engineering/mock-test)
