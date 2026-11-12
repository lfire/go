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
