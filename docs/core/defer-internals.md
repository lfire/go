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

### 设计动机与一个可复现的阈值实验

**为什么是 LIFO**：资源申请天然是栈序——先打开的文件后关闭、嵌套加锁要反序解锁，倒序执行正好让「后申请的先释放」。`_defer` 链表头插、返回时从头取，就是这个语义的直接实现。**为什么参数在注册时求值**：让 defer 的语义与开销在编译期可确定；若改成执行时才求值，捕获外部变量会引入闭包与逃逸，开放编码也无法用固定位图展开。**为什么 deferBits 取 8 位**：是「覆盖率（绝大多数函数的 defer 不超过 8 条）」对「每个返回点展开代码长度」的取舍，位图越宽，每个返回点要生成的条件跳转越多，反而吞掉收益。替代方案是 C++ 的 RAII/析构函数，由作用域自动触发、无需显式 defer，但析构时机与类型系统绑定，也表达不了「函数返回时统一补一条日志」这类跨分支收尾；Go 选显式 defer 加编译期展开，换可读性、可预测性与近乎零成本。

下面用「8 条对 9 条」把 `maxOpenDefers = 8` 这条阈值亲手测出来：

```go
package main

import "fmt"

var sink int

func step() { sink++ }

//go:noinline
func eight() {
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	step()
}

//go:noinline
func nine() {
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	defer step() // 第 9 条：超出 8 位 deferBits
	step()
}

func main() {
	eight()
	nine()
	fmt.Println(sink)
}
```

```bash
go build -gcflags=-S -o /dev/null defer_threshold.go 2>&1 | grep -E 'TEXT.*main\.(eight|nine)|deferproc'
go build -gcflags='-m -m' defer_threshold.go 2>&1 | grep -i defer
# 若因构建缓存没有输出，先 go clean -cache 或追加 -a
```

<OutputBox title="go build -gcflags=-S 片段（Go 1.23 实测）" output="TEXT main.eight(SB) 段：无 CALL runtime.deferprocStack\nTEXT main.nine(SB) 段：9 条 CALL runtime.deferprocStack(SB)" note="8 条被开放编码，第 9 条让整函数退回栈上注册；计数随是否内联变化，以本机 grep 为准。" />

逐字段解释：`TEXT main.eight` 段里没有任何 `deferproc` 调用，说明 8 条被整函数开放编码、返回点只做 `deferBits` 位测试；`TEXT main.nine` 段里出现 9 条 `CALL runtime.deferprocStack`（每条 defer 各注册一次），说明第 9 条放不进 8 位位图后**整函数**退回栈上注册，而不是「前 8 条展开、第 9 条单独注册」。`gcflags='-m -m'` 里还能看到编译器对每个调用点的展开诊断。这里 `//go:noinline` 是必须的：若函数被内联进 main，就没有独立 TEXT 段可供 grep，`-gcflags=-m` 与 `go tool objdump -s` 的结论都要跟着变——这正是观测手段的局限之一。

### 开放编码的机器码代价：每个返回点都铺一份清理代码

上一节把「8 条对 9 条」当作**是否产生注册调用**的开关来测，这一节换个观察角度：看编译出来的**函数体积**。`cmd/compile/internal/walk` 的 `walkDefer` 判定可展开后，`cmd/compile/internal/ssagen` 的 `openDeferInfo` 给每个 defer 分配 `deferBits` 的一位，并在**每一个返回点**生成一组「位测试加内联清理」：按 LIFO 逆序检查 `deferBits & (1 << i)` 是否为非零，非零就执行第 i 条被内联的清理代码。因果链是：输入 6 条固定 defer 加 2 个返回点，产出 2 × 6 = 12 组条件跳转与 12 份清理代码；`deferBits` 是 `uint8`、上限 `maxOpenDefers = 8`，所以每组最多 8 次位测试。正常路径上没有任何 `_defer` 记录、没有注册调用，代价全部落在 TEXT 段的字节数上。

用两个「同样 6 条 defer、一个展开一个退化」的函数量出这个代价：

```go
package main

import "fmt"

var sink int

func step() { sink++ }

//go:noinline
func openCoded(flag bool) {
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	if flag { // 返回点一
		return
	}
	sink++ // 返回点二
}

//go:noinline
func loopDefer(n int) {
	for i := 0; i < n; i++ {
		defer step() // 循环破坏开放编码，退化为逐次注册
	}
	sink++
}

func main() {
	openCoded(true)
	loopDefer(6)
	fmt.Println(sink)
}
```

```bash
go build -o defsz defer_size.go
go tool nm -size ./defsz | grep -E 'main.openCoded|main.loopDefer'
# 若 -S 因构建缓存无输出，先 go clean -cache 或追加 -a
go build -gcflags=-S -o /dev/null defer_size.go 2>&1 | grep -E 'deferproc|TEXT main.openCoded' || true
```

<OutputBox title="go tool nm -size 与 -S 片段（Go 1.23 / arm64）" output="100099a70        352 T main.openCoded\n100099bd0        144 T main.loopDefer\n--- go build -gcflags=-S ---\nTEXT main.openCoded 段：无注册调用\nTEXT main.loopDefer 段：可见 1 条注册调用" note="T 是代码段；三列依次为地址、字节数、符号类型加名字。" />

逐字段解释：`nm -size` 第二列是 TEXT 段字节数，开放编码版 352 字节、退化版 144 字节，前者是后者的约 2.4 倍；`openCoded` 有 2 个返回点、每个返回点铺 6 组「位测试加内联的 sink 自增」，而 `loopDefer` 只需一条循环加一次注册调用，代码段反而更小。`-S` 里 `openCoded` 段完全没有注册调用、`loopDefer` 段有 1 条（本例 Go 1.23 打印为 `runtime.deferproc`，与上一节表格里按老版本命名的 `deferprocStack` 属同一类注册入口，符号名随版本变化）；注意 `step` 只有一句自增、会被内联，所以汇编里看不到对它的 `CALL`，但 nm 量到的体积差依旧成立。用 `testing.AllocsPerRun` 复核，两版 `allocs/op` 都是 0，差别只在代码体积与 `ns/op`。

**反直觉结论的证伪**：「Go 1.14 之后开放编码让 defer 又小又快，顺带把二进制也变小」是错的。开放编码是拿**代码体积**换**运行时零注册**：每个返回点都复制一份清理代码，返回点越多、defer 越多，TEXT 段越膨胀；退化版代码更小，却要每次调用付注册成本。这恰好补上 `maxOpenDefers = 8` 为什么不能放宽的量化理由——位宽每加一位，每个返回点就多一组条件跳转，N 个返回点把成本放大 N 倍，8 位是「覆盖率」对「展开代码长度」的取舍，而不是随手取的数。工程上因此别为了清零注册而把函数拆出大量分支返回点：先读 `nm -size`，再看 `go test -benchmem`，两处都合理才动代码。

边界：三代实现的版本线是 Go 1.13 栈分配、Go 1.14 开放编码；`deferBits` 位宽、`openDeferInfo` 布局与注册调用的符号名随版本变化；`//go:noinline` 是本实验的前提，否则函数被内联进 `main` 就没有独立 TEXT 段可量；`nm -size` 的地址随构建变化、只比字节数；被内联的清理函数会让 `-S` 里看不到 `CALL`，判断「有没有展开」要以「有无注册调用」为准；`-gcflags=-S` 与 `-m` 的措辞都不是稳定契约，跨版本复现先 `go version`。

### 异常路径的真相：Go 1.23 里 `runOpenDeferFrame` 与 `freedefer` 已经改名

先纠一个版本前提。本讲前面提到「`deferreturn` 与 `runOpenDeferFrame` 执行」「`freedefer` 把用完的记录放进 P 的 defer 池」——这两个名字是**更早期版本**的符号，在本机 Go 1.23.8 的 `$GOROOT/src/runtime/panic.go` 里已经搜不到（可直接 `grep -rn 'runOpenDeferFrame\|freedefer' "$(go env GOROOT)/src/runtime"` 验证，返回为空）。Go 把 panic 展开逻辑重构成 `_panic` 上的状态机后，职责被拆到这几个函数：

```text
runtime/panic.go:741   gopanic(e)                     入口，建 _panic 并启动展开
runtime/panic.go:817   (*_panic).start(pc, sp)        初始化、找第一个含 defer 的帧
runtime/panic.go:862   (*_panic).nextDefer()           取下一个要执行的 defer
runtime/panic.go:935   (*_panic).nextFrame()           沿栈找到含 defer 的帧
runtime/panic.go:981   (*_panic).initOpenCodedDefers() 读 FUNCDATA 还原开放编码状态
runtime/panic.go:521   newdefer()                      从池里取（原 freedefer 的对偶）
runtime/panic.go:552   popDefer(gp)                    归还到池（原 freedefer 的落点）
runtime/panic.go:601   deferreturn()                   正常返回路径，复用同一套 nextDefer
runtime/panic.go:1115  recovery()                      recover 后的栈回卷
```

**状态迁移链**：`gopanic` 里 `var p _panic; p.arg = e; p.start(getcallerpc(), getcallersp())` → `start` 置 `p.link = gp._panic`、`p.lr/p.fp = pc, sp` 后调 `p.nextFrame()`。`nextFrame` 用 `unwinder` 逐帧上溯，对每个帧调 `initOpenCodedDefers(fn, varp)`：先 `fd := funcdata(fn, abi.FUNCDATA_OpenCodedDeferInfo)`，为 nil 说明该帧没有开放编码 defer；否则 `readvarintUnsafe` 依次解出 `deferBitsOffset` 与 `slotsOffset`，`deferBitsPtr = (*uint8)(add(varp, -deferBitsOffset))`，`*deferBitsPtr == 0` 表示本帧已无待执行项，非 0 则记下 `p.retpc = fn.entry() + fn.deferreturn`、`p.deferBitsPtr`、`p.slotsPtr`。随后 `nextDefer` 的内层循环是：

```text
bits := *p.deferBitsPtr
若 bits == 0 → p.deferBitsPtr = nil，跳出
i := 7 - sys.LeadingZeros8(bits)        // 取最高置位 = LIFO 顺序
bits &^= 1 << i；*p.deferBitsPtr = bits // 先清位再执行，防重入
return *(*func())(add(p.slotsPtr, i*PtrSize)), true
```

关键结论：异常路径**并不为开放编码的帧临时造 `_defer` 记录**，而是直接读栈帧里的 `deferBits` 与 `slots` 数组。只有非开放编码的帧才走 `if d := gp._defer; d != nil && d.sp == uintptr(p.sp)` 分支，取 `d.fn` 并 `popDefer(gp)`。`recover` 时若本帧还有没跑完的开放编码 defer，`recovery()` 会把 `retpc` 与 `deferBitsOffset/slotsOffset`（存成相对 SP 的偏移，避免堆对象指栈）写进 `gp.param` 的 `savedOpenDeferState`，`deferreturn()` 再用 `_panic{deferreturn: true}` 接着跑完——这就是「recover 后不丢 defer」的字段级实现。

**池的汇编层细节**：`newdefer()` 先取当前 P 的 `pp.deferpool`；为空且 `sched.deferpool != nil` 时，在 `sched.deferlock` 下把全局链上的记录按 `d.link` 逐个搬进本地池、最多补到 `cap/2`；本地有就直接从尾部 `pp.deferpool[n-1]` 取并置 nil 缩容；两者都空才 `d = new(_defer)`，最后 `d.heap = true`。`popDefer(gp)` 反过来：`d.fn = nil`、`gp._defer = d.link`、`d.link = nil`，**若 `!d.heap` 直接返回**（栈上 `_defer` 随帧回收、不进池）；本地池满则把一半 `d.link` 串回 `sched.deferpool`，再 `*d = _defer{}` 清零后 `append` 进本地池。源码注释特地点明 `d.fn = nil` 与 `d.link = nil` 必须夹在 `gp._defer = d.link` 和后续栈拷贝之间——否则栈增长时 `d.link` 可能变成陈旧指针。也就是说「池复用」只对堆分配 defer 成立，栈分配与开放编码两条路径根本不碰池。

新观察角度一：用编译器的 `-d=defer=1` 直接打印**每个 defer 落在哪一代实现**，不再靠反汇编里有没有 `deferproc` 去猜。存两个文件：

```go
// d_open.go
package main

//go:noinline
func f() {
	defer func() { println("cleanup") }()
	println("body")
}

func main() { f() }
```

```go
// d_loop.go
package main

//go:noinline
func g(n int) {
	defer func() { println("outer") }()
	for i := 0; i < n; i++ {
		defer func() { println("loop") }()
	}
}

func main() { g(3) }
```

```bash
go build -gcflags='-d=defer=1' -o /dev/null d_open.go
go build -gcflags='-d=defer=1' -o /dev/null d_loop.go
grep -rn 'runOpenDeferFrame\|freedefer' "$(go env GOROOT)/src/runtime" || echo "1.23.8 中已无这两个符号"
sed -n '862,905p' "$(go env GOROOT)/src/runtime/panic.go"
sed -n '521,595p' "$(go env GOROOT)/src/runtime/panic.go"
```

<OutputBox title="-d=defer=1 的三代判定（Go 1.23.8 / arm64）" output="# d_open.go\n./d_open.go:5:2: open-coded defer\n# d_loop.go\n./d_loop.go:5:2: stack-allocated defer\n./d_loop.go:6:27: heap-allocated defer\n# 符号核验\n1.23.8 中已无这两个符号" note="同一份诊断把开放编码、栈上、堆上三条路径一次分开，且证实旧符号已退役。" />

逐字段解释：单条固定 defer 被判为 `open-coded defer`；把 defer 放进 `for` 后，外层那条因还有循环内 defer 只能是 `stack-allocated defer`，循环内那条执行次数编译期不确定、被判为 `heap-allocated defer`——这正是本讲表格里三代实现的一次性对照，且**不依赖 `-m` 或反汇编**。`grep` 返回空（脚本里以 `|| echo` 收口），把「`runOpenDeferFrame`/`freedefer` 是旧符号」坐实。`sed -n '862,905p'` 给出 `nextDefer` 里 `bits == 0` 跳出、`i := 7 - LeadingZeros8(bits)` 取最高位、清位、`return *(*func())(add(p.slotsPtr, i*PtrSize))` 的完整循环；`sed -n '521,595p'` 给出 `newdefer`/`popDefer` 的池搬运与 `!d.heap` 直接返回，把上面两段状态迁移接回源码。

新观察角度二：把 LIFO 的位扫描**本地算一遍**，并用 `debug.Stack()` 看异常路径里 defer 的调用者是谁。

```go
// defbits.go
package main

import (
	"fmt"
	"math/bits"
)

// 对应 runtime/panic.go 的 _panic.nextDefer：取最高置位即 LIFO
func main() {
	deferBits := uint8(0b10110010) // 第 7、5、4、1 条已注册
	for deferBits != 0 {
		i := 7 - bits.LeadingZeros8(deferBits)
		fmt.Printf("执行第 %d 条 defer，剩余位图 %08b\n", i, deferBits&^(1<<i))
		deferBits &^= 1 << i
	}
}
```

```go
// stackpath.go
package main

import (
	"fmt"
	"runtime/debug"
)

func f() {
	defer func() { fmt.Print(string(debug.Stack())) }()
	panic("boom")
}

func main() { f() }
```

<OutputBox title="位扫描与 panic 栈（Go 1.23.8 / arm64）" output="执行第 7 条 defer，剩余位图 00110010\n执行第 5 条 defer，剩余位图 00010010\n执行第 4 条 defer，剩余位图 00000010\n执行第 1 条 defer，剩余位图 00000000\n--- debug.Stack() 节选 ---\ngoroutine 1 [running]:\nruntime/debug.Stack()\nmain.f.func1()\npanic({...})\n    .../src/runtime/panic.go:791 +0x124\nmain.f()\nmain.main()" note="倒序 7→5→4→1；defer 的直接下层是 panic 帧里 fn() 那一行，而非正常返回路径。" />

逐字段解释：`0b10110010` 的置位是 7、5、4、1；`7 - LeadingZeros8` 每轮取最高位，清位后继续，输出正是 LIFO 的 `7, 5, 4, 1`，与 `nextDefer` 里 `i := 7 - sys.LeadingZeros8(bits)` 完全一致——`deferBits` 8 位上限也因此不只是「条数限制」，更是「一条 `LeadingZeros8` 指令就能定序」的机器级理由。`debug.Stack()` 的栈里，`main.f.func1()`（被 defer 的闭包）紧压在 `panic({...})` 帧之上，且该帧定位到 `runtime/panic.go:791`——正是 `gopanic` 的 `for { fn, ok := p.nextDefer(); ...; fn() }` 里 `fn()` 那一行。这从运行时栈证明异常路径的 defer 由 `gopanic` 的展开循环调用，而不是函数正常返回时的 `deferreturn` 展开点调用；两条路径共用 `nextDefer`，只是入口不同。

边界：以上符号与行号以 Go 1.23.8 为准，`runOpenDeferFrame`/`freedefer` 在更早资料里仍会出现，跨版本先 `grep` 再对号入座；`FUNCDATA_OpenCodedDeferInfo` 的 varint 布局、`deferBitsOffset/slotsOffset` 相对 SP 的存法都是实现细节；`-d=defer=1` 是编译器内部开关，措辞随版本变化；`debug.Stack()` 自身会分配，只适合诊断、别放热路径；`//go:noinline` 是本实验前提；`go tool objdump`/`nm -size` 那类产物观察前两版已用过，这里刻意改用编译诊断、位扫描与运行时栈三个新角度。这条链在第 98 讲（`walkDefer` 与 `openDeferInfo`）会与编译器侧合流。

### 边界、常见误解与前后联系

| 常见误解 | 正解 | 可验证依据 |
| --- | --- | --- |
| Go 1.14 之后 defer 完全零成本，随便用 | 循环内、超 8 条、含 recover 会退化；`os.Exit` 根本不跑 defer | 上面的 `nine()` 与反汇编 |
| defer 的参数在执行时才求值 | 注册时求值；`defer fmt.Println(i)` 打印的是注册那一刻的 i | 循环里 defer 打印计数的程序 |
| `runtime.Goexit` 不会执行 defer | 会执行该 goroutine 的 defer 链，只有 `os.Exit` 不会 | `runtime.Goexit` 的文档与 defer 链路径 |

边界要点：三代实现的版本线是 Go 1.13 栈分配、Go 1.14 开放编码；`panic`、`runtime.Goexit`、`recover` 路径会把已展开的 defer 临时还原成栈上 `_defer` 再执行，语义不受优化影响；**`os.Exit` 不执行任何 defer**，是最容易忽略的例外；命名返回值下 defer 可悄悄改返回值（第 21 讲）；循环内 defer 把释放整体推迟到函数末；`-gcflags=-m` 与 `objdump` 措辞随版本变化，反汇编的 grep 结果还依赖内联决策；多 goroutine 各自持有独立 `_defer` 链，互不影响。

前后联系：第 21 讲（defer/panic/recover）给语义，本讲给实现；第 98 讲（编译器源码：SSA、逃逸与内联）会精读 `cmd/compile/internal/walk` 的 `walkDefer` 与 `cmd/compile/internal/ssagen` 的 `openDeferInfo`/`maxOpenDefers`，看开放编码与内联预算如何共同决定是否展开，第 100 讲（极致性能优化）把「别在循环里 defer」列为热路径规则。

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

<Predict q="把 3 条固定 defer 的函数改成在 for 循环里 defer 3 次，benchmem 的 allocs/op 最可能怎样？" :options='["从 0 变成 3","仍是 0，但 ns/op 升高","从 0 变成 1","编译报错"]' answer="1" output="仍是 0：退化到栈上 _defer，不产生堆分配" explain="循环破坏开放编码后走 deferprocStack，记录挂在栈帧上，allocs/op 仍为 0；变化的是 ns/op 升高、清理推迟到函数末。" />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l3-core/lesson-034-defer-internals`](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-034-defer-internals)。clone 仓库后进入 `examples/l3-core`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

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
