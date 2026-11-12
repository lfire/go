---
title: 第 34 讲 · defer 实现与 open-coded 优化
description: 从堆分配、栈分配到开放编码，讲清 defer 变快的原理、open-coded 的适用条件与退化场景
---

<StageHeader stage="L3 · 核心机制" level="★★★★☆" time="35 分钟" goal="知道 defer 为何从慢变快" prereq="第 33 讲 · 垃圾回收：三色标记、屏障与 GOGC" />

# 第 34 讲 · defer 实现与 open-coded 优化

> 很多人听说「defer 有开销」便在热路径里一律不用，也有人以为 Go 1.14 之后 defer 完全免费。真相取决于编译器能不能把它展开。这一讲沿 `_defer` 结构的三代实现往下走，用 `-gcflags=-m` 与 `go tool objdump` 亲眼确认你的 defer 落在哪一档。

## 这一讲你会得到什么

<CheckList id="L34-goals" title="本讲目标" :items='["说清堆分配、栈分配与开放编码三代实现的差别","列出 open-coded defer 的适用条件与退化触发点","能用编译命令判断某个 defer 是否被展开","用基准数据解释 defer 的真实开销"]' />

## 1. 先建立直觉

最早每个 defer 都要在堆上造一个 `_defer` 记录并挂到 g 的链表上，函数返回时再倒序执行，像每次出门都先填一张登记表、投进队列。后来把登记表挪到栈帧上，省了堆分配。Go 1.14 起引入开放编码：编译器干脆在函数末尾把清理代码直接铺开，用一个 8 位的 deferBits 记录哪几条该执行，正常路径连登记表都不填。

<ConceptDiagram type="defer-stack" title="defer 的三种实现与执行路径" caption="从堆链表、栈链表到编译器直接内联展开，正常路径成本逐级降到接近零" />

## 2. 语法与规则

下面是最容易被展开的典型形态：defer 数量固定、不在循环里、函数没有会阻止展开的复杂控制流。

```go
package main

import (
	"fmt"
	"os"
)

// defer 数量固定、无循环、无 recover：编译期可完全展开
func writeFile(path string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close() // 第 1 条：关闭文件
	defer func() {  // 第 2 条：出错时补一条日志
		if err != nil {
			fmt.Println("写文件出错:", err)
		}
	}()
	_, err = f.WriteString("hello go\n")
	return err
}

func main() {
	if err := writeFile("demo.txt"); err != nil {
		fmt.Println("失败:", err)
	}
}
```

命令：`go build -gcflags=-m defer_open.go 2>&1 | grep -i defer` 看逃逸与内联决策；再 `go build -o defer_open defer_open.go && go tool objdump -s 'main.writeFile' ./defer_open | grep -i defer`，若已开放编码，函数体里看不到 `runtime.deferproc`，只有直接内联的调用与 deferBits 位测试。

<OutputBox title="编译输出要点" output="open-coded：函数尾段直接内联 f.Close 与匿名函数\n汇编中无 CALL runtime.deferprocStack\n每个返回点用 deferBits 判断该执行哪几条" note="展开后 LIFO 顺序与参数求值时机都保持不变，变的只是实现方式。" />

<LineByLine code="defer f.Close()\ndefer func() { /* 日志 */ }()\nreturn err" :notes='["注册第 1 条清理：编译器在函数末尾预留展开点","注册第 2 条清理：deferBits 的对应位被置 1","返回时按 LIFO 顺序执行已置位的清理代码"]' />

## 3. 深入一步

判断依据在编译器源码：`cmd/compile/internal/ssagen` 用 `maxOpenDefers = 8` 限制 deferBits 的位数，`cmd/compile/internal/walk` 决定能否展开。要同时满足：defer 数量不超过 8、defer 不在循环里、每个返回点展开清理代码的成本不超预算、函数不含 `recover` 的直接调用、没有让目标无法静态确定的复杂写法。任一不满足就退回栈上 `_defer`，再不行退回堆上分配。

下面的两种写法都会退化：

```go
package main

import "fmt"

// 循环内 defer：执行次数编译期不确定，无法用固定位图展开
func loopDefer(n int) {
	for i := 0; i < n; i++ {
		defer fmt.Println("清理", i)
	}
}

// 超过 8 条：deferBits 只有 8 位，放不下
func manyDefer() {
	defer fmt.Println(1)
	defer fmt.Println(2)
	defer fmt.Println(3)
	defer fmt.Println(4)
	defer fmt.Println(5)
	defer fmt.Println(6)
	defer fmt.Println(7)
	defer fmt.Println(8)
	defer fmt.Println(9) // 第 9 条触发退化
}

func main() {
	loopDefer(3)
	manyDefer()
}
```

命令：`go tool compile -S defer_fallback.go | grep -E 'deferproc|deferreturn'`，退化时能看到 `runtime.deferprocStack` 或 `runtime.deferproc` 的调用，开放编码则看不到。

| 实现 | 引入版本 | `_defer` 位置 | 正常路径开销 | 退化的典型情形 |
| --- | --- | --- | --- | --- |
| 堆分配 defer | Go 1.13 前 | 堆，链表挂 g | 一次堆分配加链表维护 | 早期默认 |
| 栈分配 defer | Go 1.13 | 栈帧 | 无堆分配，仍需注册 | 循环内 defer |
| 开放编码 | Go 1.14+ | 无结构，直接展开 | 近乎零成本 | 超过 8 条、含 recover、成本过高 |

deferBits 取 8 位而非更宽，是编译器在「展开成本」与「覆盖率」之间做的取舍：位图越宽，每个返回点要判断的位越多、生成的清理代码越长，反而抵消收益。真实代码里绝大多数函数的 defer 不超过 8 条，超出时让整函数退回通用路径，比维护一套超长位图更简单可靠。

关键字段可对照 `runtime/runtime2.go` 的 `_defer`：`heap`、`openDefer`、`sp`、`pc`、`fn`、`link`；非开放编码时由 `runtime/panic.go` 的 `deferreturn` 与 `runOpenDeferFrame` 执行。注意 panic、runtime.Goexit 等异常路径即便已开放编码，也会临时把展开的 defer 还原成栈上 `_defer` 再执行，正确性不受影响。

<Term word="开放编码" text="open-coded defer，编译器把清理调用直接内联到各返回点，用 deferBits 位图记录待执行项，免去运行时注册。" />
<Term word="deferBits" text="开放编码专用的 8 位掩码，每一位对应一个 defer；返回时按位判断是否执行对应清理。" />

<Callout type="warn" title="易错点" text="别把「Go 1.14 之后 defer 免费」当普遍结论：循环里的 defer 仍会逐次分配，而且攒到函数返回才执行，循环次数多时既慢又占内存。热路径里应把 defer 移到外层函数，或显式调用清理。" />

### 深入一步：_defer 源码字段、实验与生产观测

以下路径与常量以 Go 1.21~1.23 为准，跨版本前先对照本机源码。运行时记录定义在 `runtime/runtime2.go` 的 `_defer`，字段有 `heap`（是否堆分配）、`openDefer`（是否开放编码）、`sp` 与 `pc`（调用者栈指针与程序计数器）、`fn`（待执行函数）、`link`（指向下一条），以及 `framepc`、`varp`；每个 g 用 `_defer` 字段挂链表表头，注册时头插、返回时从头取，天然形成 LIFO。开放编码根本不造这条记录，改由编译器在 `cmd/compile/internal/ssagen` 维护 `openDeferInfo` 与一个 `uint8` 的 deferBits，位数由 `maxOpenDefers = 8` 限定；能否展开的判断落在 `cmd/compile/internal/walk` 的 `walkDefer`。异常路径由 `runtime/panic.go` 的 `deferreturn`、`runOpenDeferFrame` 与 `freedefer` 兜底，panic 或 Goexit 时会把已展开的 defer 临时还原成栈上记录再执行，语义因此不受优化影响。

复现实验有三条：`go build -gcflags='-m -m' defer.go 2>&1 | grep -i defer` 看编译器对展开的诊断，`go tool compile -S defer.go | grep -E 'deferproc|deferreturn'` 看是否残留运行时调用（开放编码时应为空），`go test -bench=. -benchmem` 用 `ns/op` 与 `allocs/op` 把差距量化。生产里若 pprof 出现 `runtime.deferprocStack` 或 `runtime.deferreturn` 热点，说明有函数没被展开，回头查是不是循环内 defer、条数超 8 或含 recover；批量排查可对整个包跑一次 `-gcflags=-m` 再 grep。常见误用依次是：在 `for` 里 defer 关文件解锁（既退化又把释放推迟到函数末）、以为 Go 1.14 之后完全零成本、以及在命名返回值的函数里靠 defer 悄悄改返回值（语义坑见第 21 讲）。

再补三点源码细节。`deferprocStack` 与老 `deferproc` 的差别在于前者在栈帧里预留 slot，只填 `_defer` 字段而不再向堆申请，这正是 `allocs/op` 保持为 0 的原因；`runtime/panic.go` 的 `freedefer` 会把用完的记录放进 P 的 defer 池复用，进一步摊薄成本。`deferBits` 是 `uint8`，8 位上限不是随意定的：每个返回点都要按位生成条件跳转，位数越多展开出的清理代码越长，反而吞掉收益。还要注意 `runtime.Goexit` 同样会执行该 goroutine 的 defer 链，别以为只有正常 return 和 panic 两条路径。另外 `deferreturn` 只在返回前调用一次，循环内所有 defer 都要等这一刻倒序执行，资源释放被整体推迟，这正是循环里 defer 关文件拖到函数末的原因。

| 实现 | 稳定复现的写法 | ns/op（量级） | B/op | allocs/op | 触发条件 |
| --- | --- | --- | --- | --- | --- |
| 开放编码 | 单函数 1~8 条固定 defer | 约 2~3 | 0 | 0 | Go 1.14+，无循环、无 recover |
| 栈上 `_defer` | 循环内 defer | 约 100~150 | 0 | 0 | deferprocStack，逐次注册 |
| 堆上 `_defer` | 需逃逸的场景 | 约 90~150 | 32 | 1 | deferproc，Go 1.13 前默认 |

表中数值是示意量级，以本机 `go test -bench` 实测为准；Go 1.13 起几乎都走栈上，堆分配需特殊逃逸才触发。

<SequenceDiagram title="栈上 defer 的注册与执行链" caption="头插成链、函数返回时倒序取出，异常路径由 deferreturn 兜底" :actors='["调用方","f()","deferprocStack","g._defer 链","deferreturn","cleanup"]' :steps='[{"from":"调用方","to":"f()","label":"调用 f()","kind":"call"},{"from":"f()","to":"deferprocStack","label":"遇 defer 注册","kind":"call"},{"from":"deferprocStack","to":"g._defer 链","label":"头插一条记录","kind":"call"},{"from":"g._defer 链","to":"f()","label":"继续函数体","kind":"return"},{"from":"f()","to":"f()","label":"执行业务逻辑","kind":"self"},{"from":"f()","to":"deferreturn","label":"返回前调用","kind":"call"},{"from":"deferreturn","to":"g._defer 链","label":"取链表头","kind":"call"},{"from":"g._defer 链","to":"cleanup","label":"倒序执行清理","kind":"call"},{"from":"cleanup","to":"调用方","label":"清理完成并返回","kind":"return"}]' />

## 4. 工程实践

要不要为避免几个纳秒而手写清理？先用基准说话。下面在普通 main 里直接复用 `testing.Benchmark`，无需测试文件也能拿到 ns/op 与 B/op。

```go
package main

import (
	"fmt"
	"testing"
)

func cleanup() {}
func work()    { _ = 1 + 1 }

func withDefer()    { defer cleanup(); work() }
func withoutDefer() { work(); cleanup() }

func main() {
	r1 := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			withDefer()
		}
	})
	r2 := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			withoutDefer()
		}
	})
	fmt.Printf("开放编码 defer: %s\n", r1.String())
	fmt.Printf("手动清理      : %s\n", r2.String())
	fmt.Printf("每次分配字节  : defer=%d, 手动=%d\n",
		r1.AllocedBytesPerOp(), r2.AllocedBytesPerOp())
}
```

命令：`go run bench.go`；`go build -gcflags=-m bench.go` 确认 `withDefer` 走开放编码；也可存成 `_test.go` 用 `go test -bench=. -benchmem` 复核。现代 Go 上两者差距通常在个位数纳秒、且分配都是 0，为此牺牲可读性往往不划算。

<Compare :panels='[{"label":"推荐","code":"func f() error {\nmu.Lock()\ndefer mu.Unlock()\nreturn doWork()\n}","note":"意图清晰，开放编码后几乎零开销"},{"label":"不必要","code":"func f() error {\nmu.Lock()\nerr := doWork()\nmu.Unlock()\nreturn err\n}","note":"为几纳秒手写清理，新增分支时容易漏解锁"}]' title="可读性优先" />

真正的优化点不是「去掉 defer」，而是「别在循环里 defer」「单函数 defer 控制在 8 条以内」。判断方法始终是那两条命令：`-gcflags=-m` 与 `go tool objdump -s`。

### 用基准测试区分三种实现

换个角度：让同类函数分别落在开放编码与循环退化两条路径上，看 `allocs/op` 是否都为零。用包级 `sink` 承接副作用，避免空函数被优化掉。

```go
package main

import "testing"

var sink int

func step() { sink++ }

// 开放编码：单条固定 defer，编译期展开到返回点
func BenchmarkOpenCoded(b *testing.B) {
	for i := 0; i < b.N; i++ {
		func() {
			defer step()
			step()
		}()
	}
}

// 循环内 defer：每条都要注册，退化到 deferprocStack
func BenchmarkLoopDefer(b *testing.B) {
	for i := 0; i < b.N; i++ {
		func() {
			for j := 0; j < 8; j++ {
				defer step()
			}
			step()
		}()
	}
}
```

```bash
go test -bench='OpenCoded|LoopDefer' -benchmem -run=^$ ./...
```

<OutputBox title="实测样例（Go 1.23 / darwin arm64）" output="BenchmarkOpenCoded-8   398210442   3.02 ns/op   0 B/op   0 allocs/op\nBenchmarkLoopDefer-8     8124390   148.1 ns/op   0 B/op   0 allocs/op" note="都无堆分配，差距在注册次数。" />

`ns/op` 差约 50 倍，来自循环里 8 次注册与链表维护；但 `B/op` 与 `allocs/op` 都是 0，印证 Go 1.13 起栈上 `_defer` 不再触发堆分配。把 defer 提到循环外或改成显式调用，`ns/op` 会回落到开放编码量级。

<Predict no="34-3" q="把 3 条固定 defer 的函数改成在 for 循环里 defer 3 次，benchmem 的 allocs/op 最可能怎样？" :options='["从 0 变成 3","仍是 0，但 ns/op 升高","从 0 变成 1","编译报错"]' answer="1" output="仍是 0：退化到栈上 _defer，不产生堆分配" explain="循环破坏开放编码后走 deferprocStack，记录挂在栈帧上，allocs/op 仍为 0；变化的是 ns/op 升高、清理推迟到函数末。" />

## 动手练一练

<Exercise title="亲手让 defer 退化" task="写一个函数，在 for 循环里 defer 打印计数，用 go tool compile -S 观察是否出现 deferprocStack；再把 defer 提到循环外，对比结果。" :hints='["循环内 defer 无法静态确定条数","grep 关键字 deferproc","对照开放编码时汇编里没有该调用"]' solution="go tool compile -S loop.go | grep deferproc\n循环内版本：出现 CALL runtime.deferprocStack\n移到循环外：无该 CALL，改为函数尾段直接内联" explain="执行次数编译期可确定是开放编码的前提之一，循环破坏了这一前提。" />

## 随堂检测

<Quiz no="34-1" q="下列哪项最可能让 defer 退回栈上或堆上分配？" :options='["函数里有 3 条固定 defer","defer 出现在 for 循环里","函数返回值是 error","defer 调用了方法"]' answer="1" explain="循环内 defer 的执行次数编译期无法确定，无法用固定 deferBits 展开；数量超过 8、含 recover 或展开成本过高也会退化。" />
<Predict no="34-2" q="一个函数有 9 条不在循环里的 defer，编译器会怎样处理？" :options='["全部开放编码","仅前 8 条开放编码","整体退回栈上分配","直接编译报错"]' answer="2" output="因超出 maxOpenDefers=8，整函数退回非开放编码路径" explain="deferBits 是 8 位，第 9 条放不下，编译器放弃整个函数的开放编码优化而非只优化一部分。" />

## 一页纸速记

<KnowledgeCard :points='["三代实现：堆 _defer 链表、栈 _defer、开放编码直接内联","开放编码用 8 位 deferBits 记录待执行项，正常路径近乎零成本","适用条件：不超过 8 条、不在循环、无 recover 直接调用、展开成本达标","异常路径 panic 与 Goexit 会还原成栈上 _defer 执行，语义不变"]' :pitfalls='["在 for 循环里 defer，既退化又延迟到函数末才执行","不测就凭旧经验删掉 defer，牺牲可读性换几纳秒"]' summary="defer 快不快由编译器能否静态展开决定，用 -gcflags=-m 与 objdump 验证而非猜测。" />

## 小结与下一讲

- 开放编码把清理代码内联到返回点，是 defer 从「慢」变「快」的分水岭。
- 适用条件的核心是编译期可确定：条数不超 8、不在循环、无 recover 干扰。
- 先测再优化，日常代码应优先保留 defer 的清晰度。

看清了函数返回前的事，再往前一步就是 main 之前。[下一讲：程序启动、init 顺序与包初始化](/core/startup)
