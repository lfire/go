---
title: 第 54 讲 · 基准测试、模糊测试与覆盖率
description: 用 Benchmark 与 benchstat 量化性能、用 fuzz 找边界反例、读懂内存分配指标，并破除覆盖率迷信
---

<StageHeader stage="L5 · 工程化与质量" level="★★★☆☆" time="40 分钟" goal="用数据而不是感觉谈性能与质量" prereq="第 53 讲 · Mock：gomock / sqlmock / httptest" />

# 第 54 讲 · 基准测试、模糊测试与覆盖率

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

## 动手练一练

<Exercise title="给 Sum 写一个带内存统计的基准" task="函数 Sum(nums []int) int 返回切片元素之和。请写 BenchmarkSum：打开内存分配统计、重置计时器，并保证结果不会被编译器消除。" :hints='["函数签名是 func BenchmarkSum(b *testing.B)","准备切片放在计时之前，调用 b.ReportAllocs 与 b.ResetTimer","用 _ = Sum(nums) 消费返回值"]' solution="func BenchmarkSum(b *testing.B) {\n    nums := []int{1, 2, 3, 4}\n    b.ReportAllocs()\n    b.ResetTimer()\n    for i := 0; i != b.N; i++ {\n        _ = Sum(nums)\n    }\n}" explain="准备数据不计时，结果被消费，分配统计打开，这才是可对比的基准。" />

## 随堂检测

<Quiz no="54-1" q="关于覆盖率，下面哪项说法最准确？" :options='["覆盖率到 100% 就说明没有 bug","覆盖率只说明语句被执行过，不保证断言正确","覆盖率越高，测试运行越快","covermode=count 能证明并发路径没问题"]' answer="1" explain="覆盖率是「执行过」的度量，不是「验证过」的度量；断言质量、边界与并发路径都要另外判断。" />

<Predict no="54-2" q="某函数有 2 条语句，其中 1 条从未被执行，go test -cover 的输出大致是？" :options='["coverage: 0.0% of statements","coverage: 50.0% of statements","coverage: 100.0% of statements","不输出覆盖率"]' answer="1" output="coverage: 50.0% of statements" explain="默认 set 模式按「执行过的语句数 / 总语句数」计算，1 除以 2 即 50.0%。" />

## 一页纸速记

<KnowledgeCard :points='["Benchmark 用 b.N 自适应循环，b.ReportAllocs 打开分配统计","准备数据放计时外，结果必须被消费","benchstat 靠多轮样本判断差异是否显著","Fuzz 用种子语料 + 不变量，找到反例会自动落盘成回归用例"]' :pitfalls='["把覆盖率当 KPI，写大量只调用不断言的假测试","单机跑一次就宣称性能提升","基准里没消费返回值，被编译器整段消除"]' summary="谈性能与质量要拿数字和反例说话：先有基线，再改一处，最后用对比证明。" />

## 小结与下一讲

- `b.ReportAllocs` 与 `benchstat` 让性能结论可复现、可统计检验。
- Fuzz 把边界反例自动沉淀成回归用例，比手写几个边界值更彻底。
- 覆盖率是「执行过」而非「验证过」，别把它当质量分数。

[下一讲：日志：slog / zap 与结构化采样](/engineering/logging)
