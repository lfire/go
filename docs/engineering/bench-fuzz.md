---
title: 第 054 讲 · 基准测试、模糊测试与覆盖率
description: 用 Benchmark 与 benchstat 量化性能、用 fuzz 找边界反例、读懂内存分配指标，并破除覆盖率迷信
---

<StageHeader stage="L5 · 工程化与质量" level="★★★☆☆" time="40 分钟" goal="用数据而不是感觉谈性能与质量" prereq="第 053 讲 · Mock：gomock / sqlmock / httptest" />

# 第 054 讲 · 基准测试、模糊测试与覆盖率

> 「这段改成 Builder 应该更快」「覆盖率到 80% 就放心了」——这两句话都缺一样东西：可验证的依据。Go 把基准测试、模糊测试、覆盖率都放进了工具链，目的不是多几个命令，而是让性能和质量的讨论能落到数字和反例上。这一讲建立一套闭环：先有基线，再改一处，最后用对比证明改动真的有效。

## 这一讲你会得到什么
<CheckList id="goal-54" title="本讲目标" :items='["写出带内存分配统计的 Benchmark，并读懂 ns/op、B/op、allocs/op","用 benchstat 对比两次基准结果，判断差异是否显著","写 Fuzz 测试找边界输入，并说清覆盖率能证明什么、不能证明什么"]' />

## 1. 先建立直觉

把性能优化想成体检：先量一次血压（基线），再只改一个生活习惯（一次只动一处），过段时间再量一次（复测），最后拿两次报告给医生看差异有没有统计学意义（benchstat）。凭感觉改代码，就像不量血压就换药。模糊测试则像让机器替你穷举奇怪的病人：空字符串、超长输入、全分隔符，它会自动把能触发崩溃的输入缩小并保存成回归用例。

<ConceptDiagram type="perf-loop" title="优化闭环：基线 → 定位 → 改动 → 验证" />

## 2. 语法与规则

第一块是最小 Benchmark。注意 `b.N` 由测试框架动态调整，`b.ReportAllocs()` 才会输出内存指标，`b.ResetTimer()` 把准备数据的时间排除在外。

```go
// concat_bench_test.go —— 单文件即可 go test -bench 运行
package concat

import (
	"strings"
	"testing"
)

// Join 是被测函数（与测试同文件，保证单文件可跑）
func Join(parts []string) string {
	return strings.Join(parts, "-")
}

func BenchmarkJoin(b *testing.B) {
	parts := []string{"go", "test", "bench"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Join(parts)
	}
}
```

<OutputBox title="go test -bench=BenchmarkJoin -benchmem 运行结果" output="BenchmarkJoin-8   48239127   24.71 ns/op   16 B/op   1 allocs/op" note="ns/op 是耗时，B/op 与 allocs/op 是每次操作的分配量；具体数值随机器与版本变化，只做同机对比。" />

第二块是 Fuzz 测试。`f.Add` 提供种子语料，`f.Fuzz` 里的不变量一旦被打破，工具会把最小复现输入写进 `testdata/fuzz`，以后普通 `go test` 也会重跑它。

```go
// parse_fuzz_test.go —— 单文件即可 go test / go test -fuzz 运行
package parse

import (
	"strings"
	"testing"
)

// Count 统计分隔符个数，模糊测试用来找边界输入
func Count(s string) int {
	return strings.Count(s, ",")
}

func FuzzCount(f *testing.F) {
	seeds := []string{"", "a,b,c", ",,,", "abc"}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := Count(s)
		if got < 0 {
			t.Fatalf("计数不应为负: %d", got)
		}
		// 不变量：逗号个数不可能超过字符串长度
		if got > len(s) {
			t.Fatalf("计数 %d 超过长度 %d", got, len(s))
		}
	})
}
```

第三块把两个实现并排放进同一个 `_test.go`，交给 benchstat 判断是否真有差距。

```go
// join_bench_test.go —— 两个实现并排跑，供 benchstat 对比
package join

import (
	"strings"
	"testing"
)

// JoinPlus 用 += 拼接，每次循环都产生新字符串。
func JoinPlus(parts []string) string {
	s := ""
	for _, p := range parts {
		s += p + "-"
	}
	return s
}

// JoinBuilder 用 strings.Builder，复用底层缓冲。
func JoinBuilder(parts []string) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p)
		b.WriteByte('-')
	}
	return b.String()
}

func BenchmarkJoinPlus(b *testing.B) {
	parts := []string{"go", "test", "bench"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = JoinPlus(parts)
	}
}

func BenchmarkJoinBuilder(b *testing.B) {
	parts := []string{"go", "test", "bench"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = JoinBuilder(parts)
	}
}
```

<LineByLine title="逐行看一个 Benchmark" code="func BenchmarkJoin(b *testing.B) {\n    parts := []string{a, b, c}\n    b.ReportAllocs()\n    b.ResetTimer()\n    for i := 0; i != b.N; i++ {\n        _ = Join(parts)\n    }\n}" :notes='["基准函数固定以 Benchmark 开头，参数固定为指针","准备数据放在计时之外","打开 B/op 与 allocs/op 统计","把上面的准备耗时从计时里扣掉","循环次数由框架按稳定时长决定","结果必须被消费，避免被编译器整段消除","循环体闭合","函数闭合"]' />

### b.N、ReportAllocs、ResetTimer 为什么必须这样写

这几个细节不是仪式，每一个都对应一种会得出错误结论的失效方式。

`b.N` 由框架动态调整而不是写死循环次数，是因为单次操作可能只有几纳秒：固定跑一千次，总时长短到被时钟分辨率与调度抖动淹没，测出来的数字不可信。`go test` 会不断加大 `N`，直到单次测量稳定在约 1 秒量级，再据此换算 `ns/op`；这也解释了为什么基准要配合 `-count` 跑多轮——单轮本身就带噪声。必须写 `_ = Join(parts)` 消费结果，是因为编译器会做死代码消除：返回值若从未被使用，整个循环可能被优化成空循环，`ns/op` 漂亮却是测了个寂寞。`b.ResetTimer()` 把准备数据的耗时排除在外，否则构造大切片的时间会被摊进每次操作，预处理越重、结论越偏。`b.ReportAllocs()` 单独做成开关，是因为分配统计需要额外的运行时插桩，默认关闭以避免测量动作本身干扰结果；而 `B/op` 与 `allocs/op` 恰恰是判断 GC 压力与逃逸点的关键证据（呼应第 027 讲）。

fuzz 采用「种子语料 + 不变量」而不是穷举，同样是被输入空间逼出来的：字符串的组合无限，Go 的 fuzzing 引擎做的是覆盖引导（coverage-guided）变异——从 `f.Add` 的种子出发，优先保留能触发新代码分支的输入，逐步逼近边界。因此它擅长找「让不变量崩溃」的输入，却不能证明「没有 bug」：不变量写错或没写，引擎再跑也不会报。找到反例后自动缩小（minimize）并落盘到 `testdata/fuzz`，是因为偶发输入这次生成得出、下次未必生成得出；把它固化成语料，普通 `go test` 也会重跑，才算完成「发现即防回归」的闭环。

| 手法 | 覆盖边界的能力 | 靠什么保证 | 代价 |
| --- | --- | --- | --- |
| 手写几个边界值 | 只想得到的那几个 | 人的经验 | 组合一多必然漏 |
| 固定随机测试 | 随机命中，无引导 | 运气 | 命中率低，复现困难 |
| fuzz（覆盖引导） | 主动变异到新分支 | 覆盖率反馈加不变量 | 要写对不变量，耗 CPU |
| 生产流量回放 | 真实分布 | 采样 | 事后才发现，代价已发生 |

### 可验证实验：用 benchstat 读一次真实对比

第三块代码把两个实现并排跑，改完实现后按下面采集并对比：

```bash
go test -bench=. -benchmem -count=6 ./... | tee old.txt
# 只改一处实现，再采一次
go test -bench=. -benchmem -count=6 ./... | tee new.txt
go tool benchstat old.txt new.txt
```

```text
goos: darwin
goarch: arm64
pkg: example.com/join
cpu: Apple M3 Pro
              │   old.txt   │              new.txt               │
              │   sec/op    │   sec/op     vs base               │
JoinPlus-11     24.71n ± 2%    8.93n ± 1%  -63.86% (p=0.000 n=6)
```

逐字段看：前四行是环境元数据（系统、架构、包、CPU），提醒基准**只能同机、同负载、同 Go 版本比较**，换机器或升级工具链都必须重采基线；`sec/op` 是每次操作耗时，`n` 表示纳秒；`± 2%` 是六轮样本的相对波动，波动越大越说明有噪声；`vs base` 是相对基线的变化百分比；`(p=0.000 n=6)` 是统计检验的 p 值与样本数，p 小于 0.05 才认为差异显著、不是抖动。这正是不能「单机跑一次就宣称提升 30%」的原因：单次数字没有样本，也就没有显著性。

## 3. 深入一步

看懂输出比会写更重要：

| 指标或模式 | 含义 | 怎么解读 |
| --- | --- | --- |
| ns/op | 每次操作耗时 | 越低越好，但只能同机、同负载下比 |
| B/op | 每次操作分配字节数 | 反映 GC 压力来源 |
| allocs/op | 每次操作分配次数 | 比字节数更能定位逃逸点 |
| -covermode=set | 语句是否执行过 | 只回答「跑没跑到」 |
| -covermode=count | 语句执行次数 | 能看热点，但别当质量分 |

再补一句关于噪声：`-count` 不是越多越好，也不该只跑一次。经验上五到十轮足以让 benchstat 判断差异；轮数太少会把抖动当结论，太多则拖慢流水线。基准要固定机器与负载，把结果文件随 PR 存档，日后才能复盘当时为什么做了这个决定。

<Term word="benchstat" text="golang.org/x/perf 提供的命令行工具，对多轮基准结果做统计检验，给出差异百分比与置信判断，比肉眼比数字可靠。" />

<Callout type="warn" title="易错点：覆盖率迷信" text="覆盖率 100% 不等于没有 bug。它只说明某行被执行过，不说明断言是否正确、边界是否覆盖、并发路径是否走到。把覆盖率当 KPI，会催生大量「只调用、不断言」的假测试，把数字刷绿却什么也没保护。" />

### b.N 是怎么涨上来的：从探测循环到编译器死代码消除

`b.N` 由框架动态调整，这句话在 `src/testing/benchmark.go` 里有确定实现：`run()` 先用 `runN(1)` 跑一次探测，再不断加大 `b.N` 重复执行，直到单次测量的累计耗时达到 `-benchtime`（默认 1 秒），最后按 `ns/op = duration / b.N` 换算。每一轮 `runN` 都会 `runtime.GC()` 清掉上轮垃圾、`ResetTimer()` 重置计时，避免前一环境的残留污染测量。所以 `b.N` 不是常数，而是框架根据被测函数有多快**反推**出来的：函数越慢，收敛的 `N` 越小。

「结果必须被消费」同样能追到编译器的 SSA 后端。`_ = add(n, 1)` 里 `add` 被内联成 `n+1`，返回值从未被使用，死代码消除（DCE）会把这段计算连同调用一起删掉；把结果存进包级变量 `sink` 后，编译器无法证明「这个值没人用」，计算必须保留。这正是 `testing` 文档推荐 `var sink` 全局变量的由来——它不是习俗，是让编译器别把被测代码优化没了的手段。

可复现实验（Go 1.23.8 实测），分两组看机制。第一组用 `testing.Benchmark` 把探测序列直接打印出来：

```go
package main

import (
	"fmt"
	"testing"
)

var sink int

func work(n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += i
	}
	return s
}

func main() {
	r := testing.Benchmark(func(b *testing.B) {
		fmt.Println("本轮 b.N =", b.N)
		for i := 0; i < b.N; i++ {
			sink = work(100)
		}
	})
	fmt.Println("最终 ns/op =", r.NsPerOp(), " 收敛的 N =", r.N)
}
```

```text
本轮 b.N = 1
本轮 b.N = 100
本轮 b.N = 10000
本轮 b.N = 1000000
本轮 b.N = 35716455
最终 ns/op = 35  收敛的 N = 35716455
```

逐字段看：前四轮 `1 → 100 → 10000 → 1000000` 是按约 10 倍逐档探测，每轮都在问「这个 N 下跑了多久」；拿到 1,000,000 次耗时后，框架按实测速率线性外推到 1 秒目标，得到最后一轮 `35716455`，这一轮才是正式计入 `ns/op = 35` 的采样。`testing.Benchmark` 返回值的 `N` 是收敛后的最终次数，`NsPerOp()` 是 `duration / N`。理解了这一点，`-benchtime=100x` 能固定次数、`-count` 要多轮降噪也就不再神秘：它们都作用在这个探测/外推循环上。

第二组用汇编把 DCE 变成可对照的两段函数体：

```go
package main

var sink int

func add(a, b int) int { return a + b }

func unused(n int) { _ = add(n, 1) }

func used(n int) { sink = add(n, 1) }

func main() {
	unused(10)
	used(10)
}
```

```bash
go tool compile -S main.go    # 分别看 TEXT main.unused 与 TEXT main.used
```

```text
TEXT main.unused(SB) ... 函数体只剩： RET (R30)
TEXT main.used(SB)   ... 函数体保留： ADD $1, R0, R0
                                      MOVD R0, main.sink(SB)
```

逐字段看：`unused` 里 `_ = add(n, 1)` 被内联成 `n+1` 后，因为结果没人用，整个函数体被删到只剩一条 `RET`；`used` 里 `ADD $1, R0, R0` 是内联后的加法，`MOVD R0, main.sink(SB)` 把结果写进全局变量，编译器无法消除。**机制上不这么做会发生什么**：如果基准里写成 `_ = Join(parts)` 且 `Join` 是编译器看得见的纯函数，循环体可能整段消失，`ns/op` 漂亮却是测了个空循环；存进 `sink` 才保证被测计算真的发生。

**边界与失效条件**：探测只保证累计时长达标，几纳秒的函数会被放大到千万次，微基准结论严格绑定「被测输入、那台机器、那个 Go 版本」，不能外推到真实数据规模（误解一）；DCE 只在编译器能证明「无副作用且结果未用」时生效——涉及 I/O、channel、全局状态的调用删不掉，此时 `_ = f()` 测到的是副作用而非计算，同样不可信；带循环的纯函数未必被消除（编译器不删可能不终止的计算），所以不能靠「它没被删」推断基准有效。`ReportAllocs` 之所以单独做成开关，是因为分配统计要在运行时给每次分配插桩，默认关闭以免测量动作本身干扰结果（下一节展开）。

**版本前提**：本实验在 Go 1.23.8（darwin/arm64）实测，`testing.Benchmark` 与 `-benchtime` 的语义长期稳定，但探测的具体倍数序列、`go tool compile -S` 的汇编助记符随编译器版本与 GOARCH 变化；`-gcflags=-m` 可交叉看到 `can inline add`。

### 覆盖率计数器与 fuzz 变异共用同一套插桩：cmd/cover 改写了什么

`go test -cover` 不是「运行时数执行了几行」。编译前，`cmd/go` 会调用 `cmd/cover` 对每个参与编译的源文件做 **AST 改写**：把函数体切成若干基本块（basic block），在每块开头插入一条计数器自增（形如 `GoCover.Count[n]++`），同时生成一个描述各块位置与语句数的 `GoCover` 数组；测试跑完把计数器写成 coverprofile。于是覆盖率本质是「哪些计数器被置过位」，而 `go tool cover -func` 只是把这份 profile 按函数聚合成百分比。

关键在于 fuzz 用的是**同一套计数器**。`go test -fuzz` 的 worker（`internal/fuzz`）把覆盖率计数器当反馈信号：一个变异输入若触发了此前没亮过的计数器（走到新分支/新边），就被保留进语料库，否则丢弃——这就是「覆盖引导（coverage-guided）变异」的实现。上一节说 fuzz 擅长找让不变量崩溃的输入、不擅长证明没有 bug，机制上的依据在这里：计数器只负责把输入引向新代码，判断对错完全依赖你写下的 `t.Fatal`。

可复现实验（Go 1.23.8 实测）。对一个只测了正数分支的函数采一次覆盖率，直接读原始 profile：

```bash
go test -coverprofile=cover.out ./...
cat cover.out
go tool cover -func=cover.out
```

```text
mode: set
example.com/cov/calc.go:3.29,4.11 1 1
example.com/cov/calc.go:4.11,6.3 1 0
example.com/cov/calc.go:7.2,7.12 1 1
example.com/cov/calc.go:7.12,9.3 1 0
example.com/cov/calc.go:10.2,10.14 1 1
```

```text
example.com/cov/calc.go:3:	Classify	60.0%
total:				(statements)	60.0%
```

逐字段看：第一行 `mode: set` 是插桩模式，只记「执行过没有」；其后每行是 `文件:起始行.起始列,结束行.结束列  该块语句数  计数器值`。例如 `3.29,4.11 1 1` 表示从第 3 行 29 列到第 4 行 11 列这一块含 1 条语句、被置位 1 次；`4.11,6.3 1 0` 是 `if n < 0` 的分支体，计数为 0——这个 `0` 就是没被测到的盲区。`go tool cover -func` 把 5 个块按函数聚合：3 个置位、2 个为 0，故 `Classify` 为 60.0%。换成 `-covermode=count`，最后一列会变成真实执行次数（用来找热点）；`atomic` 模式则给计数器加原子操作，供并发测试用。

**从机制推出实践结论**：覆盖率是语句级计数，不区分分支真假、不检查断言、不覆盖并发时序，所以「100% 覆盖 = 没有 bug」在插桩层就不成立（误解一）；它能证明的只有「哪些基本块一次都没跑过」，正确用法是拿 0 计数去补盲区，边界值交给 fuzz 的不变量。可证伪的对照：把断言写成 `if got != want { t.Log(...) }` 忘了 `Fail`，所有计数器照样置位、覆盖率满分，测试却永远不红——计数器记录的是「执行」，不是「验证」。

**边界与失效条件**：插桩只覆盖参与编译的源文件，跨包调用要 `-coverpkg` 才计入；`mode: set` 把「执行 1 次」与「执行 100 万次」同等看待；fuzz 的计数器反馈是启发式，走到新分支不等于不变量写对了；插桩本身有运行时开销，因此带 `-cover` 的耗时不能当性能基线，基准要单独跑（呼应本节第一组实验）。

**版本前提**：本实验在 Go 1.23.8 实测；`-covermode` 的 `set / count / atomic` 三种模式自 Go 1.2 起提供，fuzz 自 Go 1.18 起进入工具链，`go tool cover -func` 的输出格式长期稳定。第 052 讲 `_testmain.go` 的生成物不含这些计数器，只有加了 `-cover` 才会由 `cmd/cover` 改写源码后再编译。

### fuzz 不是一个进程在跑：coordinator / worker、共享内存与语料落盘协议

前面把 fuzz 讲成「覆盖引导变异」，但没回答一个实现问题：变异出的输入把被测函数跑崩了，会不会把整个 `go test` 一起带走？`src/internal/fuzz/fuzz.go` 的 `CoordinateFuzzing` 注释（约 76 行）给了答案——它**创建多个 worker 进程**，这些 worker「用同一个二进制、同一个目录、同一套环境变量」，只是在参数列表最前面加一个 `-test.fuzzworker` 标志（`args := append([]string{"-test.fuzzworker"}, os.Args[1:]...)`，170 行）。所以 `go test -fuzz` 至少有两个角色：coordinator 负责维护语料库、挑输入、判定崩溃；worker 负责真正调用 `f.Fuzz` 的函数体。worker 崩了只是那个子进程退出，coordinator 收到消息后重启一个，主流程不中断——这正是「fuzz 能把崩溃变成一条可保存的反例」而不是「测试进程直接挂掉」的机制前提。

worker 数由 `-parallel` 决定（`opts.Parallel == 0` 时取 `runtime.GOMAXPROCS(0)`，92–94 行）。这一点可以直接从输出读出来（Go 1.23.8，darwin/arm64）：

```bash
go test -fuzz=FuzzCount -fuzztime=5s -parallel=4 -count=1 .
```

```text
fuzz: elapsed: 0s, gathering baseline coverage: 0/2 completed
fuzz: elapsed: 0s, gathering baseline coverage: 2/2 completed, now fuzzing with 4 workers
fuzz: minimizing 2711-byte failing input file
fuzz: elapsed: 0s, minimizing
--- FAIL: FuzzCount (0.27s)
```

逐字段：`gathering baseline coverage: 0/2 → 2/2` 是先把 `f.Add` 的两个种子跑一遍、建立基线覆盖；`now fuzzing with 4 workers` 里的 4 正是 `-parallel=4`，把它换成 `-parallel=1` 就变成 `1 workers`（实测过），默认则等于 `GOMAXPROCS`；随后 `minimizing 2711-byte failing input file` 说明某个 worker 用变异出的 2711 字节输入触发了 `t.Fatal`，coordinator 进入缩小阶段。

跨进程怎么传输入？`src/internal/fuzz/mem.go` 的 `sharedMem`（约 14 行）说得很具体：coordinator **为每个 worker 从临时文件 mmap 出一块共享内存**，注释写明「used to pass values to fuzz between processes」，且「provides no synchronization on its own」——同步靠 `workerComm` 的握手协议。头部 `sharedMemHeader`（39 行）有四个字段：`count`（worker 调用 fuzz 函数的次数，可被 coordinator 重置）、`valueLen`（本次要读的字节数）、`randState / randInc`（伪随机数发生器状态，保证各 worker 的变异序列不同又可复现）、`rawInMem`（缩小阶段放原始字节）。于是覆盖反馈的闭环是：worker 在函数体里跑一遍 → `coverage()`（`counters_supported.go`）读出每个边（edge）的 8 位计数器 → 通过共享内存 / 消息回传 → coordinator 用 `isCoverageSubset` 判断「这个输入是否点亮了新边」，是就留进语料库。这把第二版讲的 `cmd/cover` 计数器接到了「跨进程」这一层：**计数器是各 worker 进程内各自的，语料库才是 coordinator 唯一共享的状态**。

找到崩溃输入后的 minimize 也不是简单截断。`fuzz.go` 约 254–268 行显示 coordinator 把 crasher 发回一个 worker 做最小化，并「Disable inputC so other workers don't continue fuzzing」——缩小期间其他 worker 暂停变异。缩小算法反复尝试删字节、把字节换成更「简单」的值，每轮都要求**仍然触发同一个失败**，直到收敛。实验（Go 1.23.8；不变量是「输入长度大于 3 就失败」）：

```bash
go test -fuzz=FuzzCount -fuzztime=4s -parallel=1 -count=1 .
cat testdata/fuzz/FuzzCount/81476e3145e0ed8c
```

```text
（fuzz 输出）
fuzz: minimizing 50-byte failing input file
    Failing input written to testdata/fuzz/FuzzCount/81476e3145e0ed8c
    To re-run: go test -run=FuzzCount/81476e3145e0ed8c

（语料文件内容）
go test fuzz v1
string("0000")
```

逐字段：第一行 `minimizing 50-byte failing input file` 是缩小前的输入大小，最终落盘的是 `string("0000")`——**4 字节、全是字符 `0`**，即满足「长度 > 3」的最短输入；50 → 4 就是 minimize 的可量化收益。`go test fuzz v1` 是 `internal/fuzz/encoding.go` 里 `encVersion1` 常量定义的版本头，解析器靠它识别格式；下面每行的 `string(...)` 对应 `f.Fuzz` 函数签名里一个参数（`int(...)`、`[]byte(...)` 等同理），所以这份文件是**纯文本、可手工编辑、可进 git diff** 的，不是二进制。文件名 `81476e3145e0ed8c` 是输入内容的哈希，保证同一反例不会重复落盘。

由此推出一条可自证的闭环：普通 `go test`（不带 `-fuzz`）会把 `testdata/fuzz/` 下按 fuzz 目标名分的子目录里每个文件当 seed corpus 重跑。实测：

```bash
go test -count=1 .                                   # 不带 -fuzz
go test -run='FuzzCount/81476e3145e0ed8c' -count=1 .  # 精确重跑
```

```text
--- FAIL: FuzzCount (0.00s)
    --- FAIL: FuzzCount/81476e3145e0ed8c (0.00s)
        parse_test.go:11: 输入过长: "0000"
FAIL
```

子测试名里的哈希与语料文件名一致，说明落盘反例被注册成了一个普通的 `t.Run` 用例——这就是「发现即防回归」的实现：语料文件是源码的一部分，`-fuzz` 只是生成它的手段，之后每次 `go test` 都替你重跑。可证伪对照：把 `testdata/fuzz` 整个删掉再跑 `go test -count=1 .`，会变成 `ok`（或 `[no tests to run]`），反例消失——所以第二版说「语料必须随代码入库」，机制依据在这里。

**`-coverpkg` 与跨包插桩**：候选里的这一项正好补上 worker 覆盖反馈的边界。默认 `-cover` 只给**被测包自己**插桩；跨包调用要 `-coverpkg` 才计入。实测（两个包，`app` 调 `lib.Classify`，测试只走了 `Classify` 的 `n>0` 分支）：

```bash
go test -coverprofile=cov1.out ./app                 # 不带 -coverpkg
go test -coverpkg=./... -coverprofile=cov2.out ./app
go tool cover -func=cov2.out | tail -4
```

```text
cov1.out: coverage: 100.0% of statements   profile 里只有 app/app.go
cov2.out: coverage: 75.0% of statements in ./...   profile 里有 app/app.go 和 lib/lib.go
  example.com/covpkg/app/app.go:5:  Run       100.0%
  example.com/covpkg/lib/lib.go:3:  Classify   66.7%
  total:                            (statements)  75.0%
```

逐字段并代入算式：不带 `-coverpkg` 时只统计 `app` 的 1 条语句、命中 1 条 → 1/1 = 100.0%；加 `-coverpkg=./...` 后统计范围变成 `app`(1) + `lib`(3，`if`、`return "neg"`、`return "pos"`) 共 4 条，测试只命中 `Run`、`if`、`return "pos"` 3 条 → 3/4 = 75.0%，`Classify` 自身 2/3 = 66.7%。**同一个测试、两份覆盖率数字，差别只在插桩范围**——分母从「被测包」变成「`-coverpkg` 匹配到的所有包」。这对 fuzz 有直接后果：worker 的覆盖引导计数器也只对被插桩的包生效，若目标逻辑在另一个包却没写 `-coverpkg`，变异引擎看不到那边的边，引导就退化成近似随机，找到反例的效率大幅下降。可证伪：把 `-coverpkg` 去掉后重跑同一个 fuzz，`-fuzz` 仍会变异，但「走到新分支」的反馈少了，触发同一崩溃所需的输入数会明显上升。

**边界与失效条件**：`-parallel` 是 worker **进程**数，不是 goroutine 数，每个 worker 都是独立进程、独立地址空间，靠共享内存传值；worker 里 `t.Parallel` 无效（第 052 讲 `t.parent.barrier == nil` 那条），因为每个 worker 一次只处理一个输入以便把崩溃归因到具体输入；minimize 只在「能稳定复现同一失败」时收敛，失败依赖时间 / 随机 / 全局状态的用例可能缩小失败或落到较大的输入；`go test fuzz v1` 是文本协议，手写语料要严格符合 `类型(值)` 每行一个的格式，格式错会在 baseline 阶段报解析错误；`-coverpkg` 扩大插桩范围会同时放大运行时开销与 profile 体积。

**版本前提**：本机 Go 1.23.8（darwin/arm64）实测；fuzz 自 Go 1.18 进入工具链，`-test.fuzzworker` 多进程模型、`sharedMem` / `sharedMemHeader`、`go test fuzz v1` 版本头自 1.18 起稳定；`-coverpkg` 自 Go 1.10 起提供，`of statements in ./...` 的口径随 `-coverpkg` 参数显示。第 052 讲讲 `M.Run` 的信号量，这里讲的是**进程级**并行——两者不是一套机制，别把 `-parallel` 在 `go test` 和 `go test -fuzz` 里的含义混为一谈；第 059 讲的 trace / pprof 则是另一条采样路径。

### benchstat 凭什么说「差异显著」：从输出反推统计模型

前面实验里 `benchstat old.txt new.txt` 打出 `-63.86% (p=0.000 n=6)`，第二版解释了 `sec/op`、`± 2%`、`vs base` 的含义，但没回答 `p` 是怎么算出来的、`n=6` 为什么够。把统计模型补上，才谈得上「用数据说话」。

`benchstat`（`golang.org/x/perf`，Go 1.24 起可用 `tool` 指令装）默认对两组样本做 **Mann–Whitney U 检验**（非参数秩和检验），而不是 t 检验——因为基准耗时常常不是正态分布、还带离群点，秩和检验不假设分布。流程是：把两组共 `n1 + n2` 个观测合并排序、编秩，`R1` 为其中一组的秩和，则

```text
U1 = R1 − n1(n1+1)/2
U2 = n1·n2 − U1
U  = min(U1, U2)
μ  = n1·n2 / 2
σ  = √( n1·n2·(n1+n2+1) / 12 )
z  = (U − μ + 0.5) / σ        （加 0.5 是连续性校正）
双侧 p 由 z 查标准正态分布得到
```

代入前面那次 `-count=6` 的极端情形：两组各 `n1 = n2 = 6`，若 `new` 的 6 个耗时**全部**小于 `old`（完全分离），合并后 `new` 拿到秩 1..6，`R_new = 21`，于是 `U_new = 21 − 6·7/2 = 0`、`U_old = 36 − 0 = 36`、`U = 0`；`μ = 6·6/2 = 18`，`σ = √(6·6·13/12) = √39 ≈ 6.245`，`z = (0 − 18 + 0.5)/6.245 ≈ −2.80`，双侧 `p ≈ 0.005`。精确双侧检验是 `2 / C(12,6) = 2/924 ≈ 0.00217`。这就是输出里 `p=0.000`（按三位小数显示）的来源量级：**6 对 6 完全分离已经足够显著**，不需要更多样本。反过来，若两组数值交错，`U` 会靠近 `μ = 18`、`z` 接近 0、`p` 接近 1，此时 `vs base` 即便有个位数百分比也不能宣称有差异。

由此得到几条可证伪的实践结论：其一，`-count` 的轮数直接进 `n1 / n2`，`n` 太小（如 3 对 3）时即使完全分离，精确 `p = 2/C(6,3) = 0.1` 也过不了 0.05 门槛——这就是「五到十轮」而非「跑一次」的统计依据，公式能算出具体下界：`n1=n2=n` 完全分离时 `p = 2/C(2n,n)`，n=4 → 0.029、n=5 → 0.0079、n=6 → 0.0022。其二，`±` 那一列是**置信区间 / 离散度**而非标准差的一个随意倍数，波动大时区间变宽、`p` 变大，所以单机一次测出 `-30%` 没有意义——没有样本就没有秩和。其三，`p` 只回答「差异是否可能由随机波动产生」，不回答「差异是否值得优化」：-63.86% 与显著性都成立，仍要回到第 059 讲看这段耗时在真实请求里占多大比例。

**边界与失效条件**：Mann–Whitney U 检验的是「两组分布是否相同 / 一组是否整体偏小」，对**中位数**差异敏感、对少数极端离群点不敏感，样本严重交错时结论保守；上面的精确 `p = 2/C(2n,n)` 只在「完全分离」时成立，一般情形要用含结（tie）校正的 `σ`；两轮基准若跑在不同机器、不同 Go 版本或不同 `-gcflags` 下，`n` 再大也只是在检验环境差异，不是代码差异——环境元数据那几行正是为此存在。benchstat 对 `B/op`、`allocs/op` 等其它指标分别做同样的检验，所以一次对比里可能 `sec/op` 显著下降、而 `allocs/op` 的 `p` 不显著。

**版本前提**：本机 Go 1.23.8（darwin/arm64），benchstat 以 `golang.org/x/perf` 的实现为准（老版 `go get` 安装与新 `go tool benchstat` 的检验细节一致，均用秩和检验）；`-count` 语义、Mann–Whitney U 公式长期稳定。第 054 讲前面讲 `b.N` 自适应与 benchstat 输出读法，这里补的是 `p` 与 `n` 背后的统计模型，把「用数据而不是感觉」落到能手算的公式上。

### 三条最常见的误解

**误解一：`ns/op` 更小，就代表这个实现在所有场景都更好。** 反例：`+=` 拼接在三个元素时可能与 `strings.Builder` 相差不多，但元素上千时每次循环都分配新字符串，复杂度退化，基准却只测了那三组输入。基准的结论严格绑定「被测输入、那台机器、那个 Go 版本、那次编译」；输入规模、数据分布、Go 版本任一变化都要重测。正确做法是按真实数据规模设计多组输入，各跑一次，并让基线文件随运行环境更新。

**误解二：fuzz 跑了半小时没崩，说明函数没有 bug。** 反例：`FuzzCount` 若只断言「计数不为负」，漏掉「计数不可能超过输入长度」这条性质，引擎再怎么变异也不会报；它也不覆盖并发时序、性能与外部依赖行为。fuzz 只能打破你写下的不变量，不变量的质量就是它能力的上限。正确做法是把不变量写成「输出与输入之间的关系」（性质），而不是几个具体期望值，再配合第 052 讲的表驱动与第 049 讲的 `-race`。

**误解三：fuzz 找到的输入这次修掉就行，不用留档。** 反例：开发本地复现、改完代码、没提交语料，几个月后重构又把同一个边界改坏，CI 全绿、线上再炸一次。fuzz 语料的价值正在于把偶发反例变成永久回归用例；`testdata/fuzz` 必须随代码入库，普通 `go test` 会自动重跑它。`-covermode=count` 也常被误读：它只是给每条语句记执行次数，用来找热点，执行次数多不代表并发路径被覆盖，更不代表断言正确。

## 4. 工程实践

标准闭环是一组命令，不是一句结论：

```bash
# 多跑几轮降低噪声，先存基线
go test -bench=BenchmarkJoin -benchmem -count=6 ./... | tee old.txt

# 只改一处代码，再采一次
go test -bench=BenchmarkJoin -benchmem -count=6 ./... | tee new.txt

# 统计检验：差异是否显著、有没有回归
go install golang.org/x/perf/cmd/benchstat@latest
benchstat old.txt new.txt

# 覆盖率看趋势，模糊测试找反例
go test -cover -covermode=count ./...
go test -fuzz=FuzzCount -fuzztime=30s ./...
```

覆盖率更适合当趋势而不是门槛：给核心包设一个下限，防止新代码完全没测；但不要逐包攀比数字。更有价值的两个动作是看未被覆盖的分支是不是恰好是关键错误路径，以及每次 fuzz 新增的语料有没有沉淀成回归用例。

<Compare title="凭感觉，还是按闭环" :panels='[{"label":"写法 A · 凭感觉","code":"// 反例：看着更短就换了实现\n// 没有基线，也没法证明变快\n// 改完直接上线，回归无从发现","note":"结论不可复现，甚至可能悄悄变慢"},{"label":"写法 B · 闭环","code":"go test -bench=. -benchmem -count=6 | tee old.txt\n// 只改一处\nbenchstat old.txt new.txt\n// 用数据决定是否保留","note":"差异有统计依据，回归能被当场拦下"}]' />

几个反模式值得点名：基准测试里没消费返回值，被编译器整段优化掉，数字漂亮却什么都没测；把准备数据的耗时算进循环，`ns/op` 全是噪声；单机跑一次就宣称「提升 30%」；模糊测试跑三秒就停，边界远没探到。另外 `-count=6` 不是形式主义，`benchstat` 正是靠多轮样本判断波动是否只是噪声。

最后提醒一句：基准与模糊测试都要进 CI，但策略不同。基准适合在改动热点路径时由 PR 触发对比，模糊测试适合定时长跑，把偶发反例慢慢喂进语料库。把两者都塞进每次提交的全量门禁，只会把流水线拖到没人愿意等。

<Callout type="tip" title="一条可执行的检查" text="把 -benchmem 与 -count 写进 Makefile 的 bench 目标，任何人改了热点路径都必须贴出 benchstat 对比；没有对比数字的性能 PR 不予合入。" />

### 一次真实故障：fuzz 找到的边界，手工测试三年没发现

现象：解析用户上传的标签串时，一个线上请求让服务 panic，错误栈落在按下标取结果的位置。排查路径：

1. 从 panic 日志反查输入，是「只有分隔符」或「超长连续分隔符」这类组合，而手工用例表只写了空串、正常串、单个分隔符三种；
2. 给解析函数补 Fuzz，种子用现有语料，不变量写成两条——任何输入都不得 panic、输出标签数与输入的关系必须成立；
3. `go test -fuzz=FuzzParseTags -fuzztime=30s` 跑了几分钟就生成了触发下标越界的输入，引擎自动把它缩小成最短复现，落盘到 `testdata/fuzz/FuzzParseTags`；
4. 修复是补长度判断，并把这份语料随代码提交，此后普通 `go test` 每次都会重跑它；
5. 复盘根因：人只会枚举「想得到」的边界，而覆盖引导的变异专挑「能走到新分支」的输入，恰好命中人想不到的组合。

替代做法与代价也清楚：继续手写边界值，组合一多就爆炸且仍靠想象；固定随机测试没有覆盖引导，命中率极低；生产回放最真实，但发现时故障已经发生。fuzz 的性价比正在于把「事后发现」提前成「提交前发现」，并且把发现固化成回归。

### 把基准与 fuzz 接进 CI

```yaml
name: bench-fuzz
on:
  pull_request:
  schedule:
    - cron: '0 3 * * *'
jobs:
  quality:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: 基准对比（仅性能相关 PR）
        if: contains(github.event.pull_request.labels.*.name, 'perf')
        run: |
          go test -bench=. -benchmem -count=6 ./... | tee new.txt
          go tool benchstat bench/baseline.txt new.txt
      - name: 模糊测试定时长跑
        run: go test -fuzz=Fuzz -fuzztime=60s ./...
```

版本锁定是这里的要害：`go-version-file: go.mod` 让 CI 用仓库声明的 Go 版本，因为编译优化、`strings.Builder` 之类标准库实现、甚至调度都会随版本变，基准数字只有在同一工具链下才有可比性；`benchstat` 这类工具建议用 Go 1.24 起的 `tool` 指令（`go get -tool golang.org/x/perf/cmd/benchstat`）把版本写进 `go.mod`，再用 `go tool benchstat` 调用，本地与 CI 就不可能装到不同版本。更早的 Go 版本则把工具版本钉在 Makefile 里，由 CI 与本地执行同一条 `make bench`。基准只在改动热点路径的 PR 上对比、fuzz 用 `schedule` 定时长跑，是照着原文「别把两者塞进每次提交的全量门禁」落地的：基线文件、语料库都随环境更新，才不会让流水线慢到没人愿意等。

### 与 L10 实战工程结构的呼应

L10 里数字和反例都是硬需求：`/projects/kv-1-memory` 的 `Storage` 读写、`/projects/kv-3-async` 的 WAL 追加吞吐、`/projects/net-shortlink` 的 Base62 编解码，优化前后都要贴 benchstat 对比；`/projects/kv-2-network` 的协议帧解码、`/projects/cli-todo` 的参数解析，正是 fuzz 找畸形输入的主场，找到的语料回填成第 052 讲用例表里的一行。再往后，第 059 讲 pprof 定位到热点、改完代码，收益仍要靠本讲的闭环来证明；第 060 讲把基准与 fuzz 排进流水线不同触发条件，才让「用数据说话」变成日常而非一次性动作。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l5-engineering/lesson-054-bench-fuzz`](https://github.com/lfire/go/tree/main/examples/l5-engineering/lesson-054-bench-fuzz)。clone 仓库后进入 `examples/l5-engineering`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给 Sum 写一个带内存统计的基准" task="函数 Sum(nums []int) int 返回切片元素之和。请写 BenchmarkSum：打开内存分配统计、重置计时器，并保证结果不会被编译器消除。" :hints='["函数签名是 func BenchmarkSum(b *testing.B)","准备切片放在计时之前，调用 b.ReportAllocs 与 b.ResetTimer","用 _ = Sum(nums) 消费返回值"]' solution="func BenchmarkSum(b *testing.B) {\n    nums := []int{1, 2, 3, 4}\n    b.ReportAllocs()\n    b.ResetTimer()\n    for i := 0; i != b.N; i++ {\n        _ = Sum(nums)\n    }\n}" explain="准备数据不计时，结果被消费，分配统计打开，这才是可对比的基准。" />

## 随堂检测

<Quiz no="054-1" q="关于覆盖率，下面哪项说法最准确？" :options='["覆盖率到 100% 就说明没有 bug","覆盖率只说明语句被执行过，不保证断言正确","覆盖率越高，测试运行越快","covermode=count 能证明并发路径没问题"]' answer="1" explain="覆盖率是「执行过」的度量，不是「验证过」的度量；断言质量、边界与并发路径都要另外判断。" />

<Predict no="054-2" q="某函数有 2 条语句，其中 1 条从未被执行，go test -cover 的输出大致是？" :options='["coverage: 0.0% of statements","coverage: 50.0% of statements","coverage: 100.0% of statements","不输出覆盖率"]' answer="1" output="coverage: 50.0% of statements" explain="默认 set 模式按「执行过的语句数 / 总语句数」计算，1 除以 2 即 50.0%。" />

## 一页纸速记

<KnowledgeCard :points='["Benchmark 用 b.N 自适应循环，b.ReportAllocs 打开分配统计","准备数据放计时外，结果必须被消费","benchstat 靠多轮样本判断差异是否显著","Fuzz 用种子语料 + 不变量，找到反例会自动落盘成回归用例"]' :pitfalls='["把覆盖率当 KPI，写大量只调用不断言的假测试","单机跑一次就宣称性能提升","基准里没消费返回值，被编译器整段消除"]' summary="谈性能与质量要拿数字和反例说话：先有基线，再改一处，最后用对比证明。" />

## 小结与下一讲

- `b.ReportAllocs` 与 `benchstat` 让性能结论可复现、可统计检验。
- Fuzz 把边界反例自动沉淀成回归用例，比手写几个边界值更彻底。
- 覆盖率是「执行过」而非「验证过」，别把它当质量分数。

[下一讲：日志：slog / zap 与结构化采样](/engineering/logging)
