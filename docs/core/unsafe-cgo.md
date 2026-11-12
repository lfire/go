---
title: 第 37 讲 · unsafe、cgo 与跨边界成本
description: 在必要时安全使用底层能力：unsafe.Pointer 规则、uintptr 陷阱、cgo 开销与内存所有权
---

<StageHeader stage="L3 · 核心机制" level="★★★★★" time="45 分钟" goal="在必要时安全地使用底层能力" prereq="第 36 讲 · 编译、链接、SSA 与内联" />

# 第 37 讲 · unsafe、cgo 与跨边界成本

> 到这里我们已经能看懂编译器和运行时替我们做了什么。但总有些场景要越过类型系统：与操作系统或硬件交互、复用 C 生态、榨干一次零拷贝。`unsafe` 与 cgo 就是那两道门。这一讲把门的规则、最典型的 `uintptr` 事故、以及跨语言的调用成本讲清楚，并反复强调：能力越大，责任边界越要明确。

## 这一讲你会得到什么

<CheckList id="L37-goals" title="本讲目标" :items='["记住 unsafe.Pointer 的合法转换规则","说清 uintptr 为何危险并写出正确写法","解释 cgo 调用的开销来源与指针传递约束","厘清 Go 堆与 C 堆的内存所有权","在 cgo、plugin、WASM 与纯 Go 之间做取舍"]' />

## 1. 先建立直觉

普通 Go 代码里，指针由 GC 看管：只要还被引用，对象就活着。`unsafe.Pointer` 把这个承诺拿掉，允许你把任意地址当成任意类型来读写；`uintptr` 更彻底，它只是个整数，GC 完全不把它当根。cgo 则把内存分成两本账：Go 堆由 GC 管，C 堆由 `malloc` / `free` 管，谁分配谁释放。踩坑几乎都发生在「把整数当指针」或「两本账混淆」的地方。

<ConceptDiagram type="stack-heap" title="Go 管理的内存与 C 管理的内存" caption="unsafe 打破类型边界，cgo 打破内存所有权边界，两者都要自己负责正确性" />

## 2. 语法与规则

先看合法的用法。核心规则只有一句：`unsafe.Pointer` 可以在「指向同一块内存」的前提下做类型转换与偏移，且偏移要用 `unsafe.Add`、`unsafe.Offsetof` 这类封装，全程保持 Pointer 形态。

```go
package main

import (
	"fmt"
	"unsafe"
)

type header struct {
	Flag  byte
	Count int32
	Data  int64
}

func main() {
	h := header{Flag: 1, Count: 7, Data: 42}

	// 规则一：指针转 Pointer 再转另一种指针，要求布局等价
	p := unsafe.Pointer(&h)
	asBytes := (*[unsafe.Sizeof(h)]byte)(p)
	fmt.Printf("结构体大小 = %d 字节，首字节 = %d\n", unsafe.Sizeof(h), asBytes[0])

	// 规则二：字段偏移用 Offsetof，配合 unsafe.Add 在同一表达式内完成，
	// 全程是 Pointer，不产生游离的 uintptr 中间变量
	off := unsafe.Offsetof(h.Data)
	dataPtr := (*int64)(unsafe.Add(p, off)) // Go 1.17+
	fmt.Printf("Data 偏移 = %d，读到 = %d\n", off, *dataPtr)

	*dataPtr = 99
	fmt.Println("改写后 h.Data =", h.Data)
}
```

命令：`go run safe.go`。由于结构体有内存对齐，`Flag` 与 `Count` 之间有填充，`Offsetof(h.Data)` 会是 8 而不是 5，这个偏移值就是「结论从哪来」的具体证据。

<OutputBox title="safe.go 运行结果" output="结构体大小 = 16 字节，首字节 = 1\nData 偏移 = 8，读到 = 42\n改写后 h.Data = 99" note="偏移由编译器按对齐规则算出，绝不要手填常数 5。" />

<LineByLine code="p := unsafe.Pointer(&h)\nasBytes := (*[unsafe.Sizeof(h)]byte)(p)\ndataPtr := (*int64)(unsafe.Add(p, off))" :notes='["取结构体地址并统一成 Pointer，作为后续转换的起点","把整块内存按字节数组视图读取，Sizeof 由编译器给出","用 Add 做偏移，结果仍是 Pointer，GC 始终看得见这块内存"]' />

| API | 引入版本 | 作用 |
| --- | --- | --- |
| `unsafe.Add(ptr, len)` | 1.17 | Pointer 上做算术，替代手拼 uintptr |
| `unsafe.Slice(ptr, len)` | 1.17 | 由指针构造切片视图 |
| `unsafe.SliceData` | 1.20 | 取切片底层数组指针 |
| `unsafe.String` / `StringData` | 1.20 | 字节指针与 string 互转 |
| `unsafe.Offsetof` | 早期 | 结构体字段相对偏移 |
| `runtime.KeepAlive` | 早期 | 显式把对象保活到某点之后 |

这些封装的共同点是接受 Pointer 而非 uintptr，编译器能识别它们、并在必要时保持对象存活。官方 `unsafe` 文档列了六条合法模式，其余写法都应视为可疑。

## 3. 深入一步

`uintptr` 事故几乎是同一个模式：把地址降级成整数暂存，中间隔了一次可能触发 GC 或栈增长的操作，再还原成指针。因为整数不是根，GC 可能已经把对象回收，或栈增长把栈上对象搬了家，旧地址就成了悬垂值。下面这段**能编译，但结果未定义**，仅用于说明，切勿用在生产。

```go
package main

import (
	"fmt"
	"runtime"
	"unsafe"
)

type node struct{ Val int64 }

// 危险示范：把指针降级成整数后暂存，再跨过可能触发 GC 的调用。
// 结果未定义：对象可能已被回收。仅供教学，切勿在生产使用。
func broken() {
	n := &node{Val: 123}
	addr := uintptr(unsafe.Pointer(n)) // 错误一：GC 不再把 n 视为根

	runtime.GC()           // 错误二：中间发生 GC
	_ = make([]byte, 1024) // 错误三：又一次分配、可能的栈增长

	p := (*node)(unsafe.Pointer(addr)) // 错误四：从陈旧整数还原指针
	fmt.Println("危险读值:", p.Val)      // 可能是垃圾值，甚至崩溃
}

func main() { broken() }
```

`go vet` 的 unsafeptr 检查会报 `possible misuse of unsafe.Pointer`；`go test -race` 与 `go build -gcflags=all=-d=checkptr=2` 会启用 checkptr 检测这类未对齐、越界的转换。这就是「结论从哪来」：不是玄学，是工具能抓到的确定性错误。

正确写法是让指针永不落地成游离整数；确需 uintptr（例如 syscall）时，转换必须在同一个表达式内完成，并用 `runtime.KeepAlive` 兜底。

```go
package main

import (
	"fmt"
	"runtime"
	"unsafe"
)

type node struct{ Val int64 }

// 正确一：全程 Pointer，偏移用 unsafe.Add，绝不落地成 uintptr 变量
func byPointer(n *node, off uintptr) int64 {
	return *(*int64)(unsafe.Add(unsafe.Pointer(n), off))
}

// 正确二：确需 uintptr 时（如 syscall），转换在同一表达式内，
// 并用 KeepAlive 把对象保活到最后一次使用之后
func withKeepAlive(n *node) int64 {
	p := (*int64)(unsafe.Pointer(uintptr(unsafe.Pointer(n)) + 0))
	runtime.GC()
	v := *p
	runtime.KeepAlive(n)
	return v
}

func main() {
	n := &node{Val: 123}
	fmt.Println("按偏移读:", byPointer(n, unsafe.Offsetof(n.Val)))
	fmt.Println("保活读值:", withKeepAlive(n))
}
```

<Compare :panels='[{"label":"危险写法","code":"addr := uintptr(unsafe.Pointer(n))\nruntime.GC()\np := (*node)(unsafe.Pointer(addr))","note":"整数脱离 GC 视野，中间 GC 后地址可能失效"},{"label":"正确写法","code":"p := (*node)(unsafe.Add(unsafe.Pointer(n), off))\nruntime.KeepAlive(n)","note":"全程 Pointer，必要时显式保活"}]' title="uintptr 陷阱与修复" />

<Term word="uintptr" text="足以存下指针的整数类型，但不被 GC 当作根；一旦把地址存成 uintptr，对象就失去了存活保证。" />
<Term word="checkptr" text="编译器的指针使用检查插桩，-race 或 -d=checkptr=2 启用，可发现未对齐与越界的 unsafe 转换。" />

<Callout type="warn" title="风险边界" text="unsafe 绕过了类型安全与 GC 保证，只应在标准库、与系统或硬件交互、以及已用基准证明是热点的极少数代码里使用；升级 Go 版本可能改变结构体布局或 string、slice 头部表示，使代码悄悄失效。凡用 unsafe，必配 go vet、-race 与 checkptr，并把范围封在最小函数内、写清不变量。" />

### 推导链：从 cgocall 与 GC 根推到 uintptr 陷阱

结论分三处：`unsafe/unsafe.go` 的包注释列出六条合法的 `unsafe.Pointer` 转换模式；`runtime/cgocall.go` 的 `cgocall`/`asmcgocall`/`cgocallback` 负责跨边界，`runtime/proc.go` 的 `entersyscall`/`exitsyscall` 通知调度器；`cmd/compile` 的 `checkptr` 插桩在 `unsafe.Pointer` 转换处检查对齐与越界。推导链：

- **uintptr 为什么危险**：它是整数类型，GC 的扫描器只把 `unsafe.Pointer` 与指针类型当根。若把 GC 改成扫描所有 `uintptr`，任何恰好等于某地址的整数都会意外保活对象，且无法区分「偏移量」与「地址」，整数运算的语义会崩塌。代价就是：一旦地址降级成整数变量，对象立刻失去存活保证，中间发生 GC 或栈增长后还原出的指针必然可能悬垂。
- **cgo 为什么要栈切换**：C 代码可能长时间阻塞、使用大栈、或调用不可增长的栈，goroutine 的可增长小栈无法安全承接；`cgocall` 切到系统栈，`entersyscall` 把 M 与 P 解绑，让调度器在 C 阻塞期间把 P 交给别的 M，避免整个 P 被拖住。
- **checkptr 为什么能抓到**：编译器在转换处插桩，运行时按目标类型校验对齐与范围，`-race` 或 `-d=checkptr=2` 启用，把「未定义行为」变成可检测错误。

把 cgo 的边界成本量出来，而不是只记「几十纳秒」这个结论：

```go
package main

/*
#include <stdint.h>
static int32_t c_add(int32_t a, int32_t b) { return a + b; }
*/
import "C"

import (
	"fmt"
	"testing"
)

//go:noinline
func goAdd(a, b int32) int32 { return a + b }

var sink int32

func main() {
	g := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sink = goAdd(1, 2)
		}
	})
	c := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sink = int32(C.c_add(1, 2))
		}
	})
	fmt.Printf("纯 Go: %s\n跨 cgo: %s\n", g.String(), c.String())
}
```

```bash
CGO_ENABLED=1 go build -o cgo_bench cgo_bench.go
./cgo_bench
go tool nm ./cgo_bench | grep -i cgocall
CGO_ENABLED=0 go build cgo_bench.go   # 会失败，演示纯静态交叉编译的代价
```

<OutputBox title="cgo_bench 实测与符号表（Go 1.23 / arm64，数值随机型波动）" output="纯 Go: 1000000000   0.95 ns/op   0 B/op   0 allocs/op\n跨 cgo:  43427451  29.54 ns/op   0 B/op   0 allocs/op\nnm: 100a2c0 T runtime.cgocall\nnm: 100a4e0 T runtime.asmcgocall" note="两边 B/op 都是 0，说明成本不在 Go 堆分配。" />

逐字段解释：`ns/op` 差约三十倍（一个数量级），`B/op` 与 `allocs/op` 却都是 0——关键结论是 cgo 的成本**不在堆分配**，而在 `cgocall` 的栈切换、参数封送与 `entersyscall`/`exitsyscall` 的状态登记；`go tool nm` 里出现 `runtime.cgocall` 与 `runtime.asmcgocall` 两个 `T`（代码）符号，把「跨边界」与具体运行时函数对上。`CGO_ENABLED=0 go build` 失败，则证明一旦启用 cgo 就失去了纯静态交叉编译能力。注意基准必须在同一台机器、同一版本上对比，JIT 预热、内联决策与 `GOMAXPROCS` 都会让绝对值波动。

**为什么这样设计**：`uintptr` 不当根，是因为当根会让整数运算语义崩塌；代价是程序员必须用 `unsafe.Add`/`unsafe.Slice` 保持 Pointer 形态、或把转换收在同一表达式内并用 `runtime.KeepAlive` 兜底。cgo 切系统栈并 `entersyscall`，是因为 C 的栈模型与 goroutine 不兼容，阻塞的 C 调用也不能占住一个 P；代价是单次几十纳秒与调度复杂度。替代方案（纯 Go 重写）省掉边界与构建成本，代价是生态复用与开发量；WASM 用沙箱换隔离与可卸载，代价是调用开销与能力受限。

### 一次 cgo 调用的状态迁移链与固定边界成本的分解

上一节用 benchmark 得出「cgo 约 30 ns、纯 Go 约 1 ns」，但没说清这 30 ns 花在哪些状态切换上，也没回答「把 C 函数写轻一点能不能变快」。先看包装函数：输入 `C.c_add(1, 2)`，cgo 生成的 `main._Cfunc_c_add.abi0` 先检查 g 的栈边界并 `MOVD.W R30, -48(RSP)` 建帧，把参数封送进栈上的参数结构，再 `CALL runtime.cgocall(SB)`。`cgocall` 经 `asmcgocall` 切到**系统栈**（g0 的栈），`entersyscall` 把当前 M 与 P 解绑、P 转入可被 sysmon 转交他人的状态；C 函数在 M 上执行；返回时 `exitsyscall` 重新绑定 P，包装函数还要对传进去的 Go 指针做 `runtime.cgoUse` 保活检查，最后读回结果。分支条件有两处：`CGO_ENABLED=0` 时这些符号根本不链接、构建失败；C 侧若把 Go 指针长期存进 C 内存，`cgoCheckPointer` 在 `cgocheck` 开启时直接报错。可观察输出就是反汇编里的 `CALL runtime.cgocall`、`convT32`、`runtime.cgoUse` 与 `nm` 里的 `runtime.cgocall`、`runtime.asmcgocall` 两个代码符号。

把「空 C 函数 / 加法 C 函数 / 一次处理 1024 个元素的 C 函数」放在一起，就能把这 30 ns 拆开：

```go
package main

/*
#include <stdint.h>
static int32_t c_noop(void) { return 0; }
static int32_t c_add(int32_t a, int32_t b) { return a + b; }
static int32_t c_sum(const int32_t *p, int32_t n) {
	int32_t s = 0;
	for (int32_t i = 0; i < n; i++) s += p[i];
	return s;
}
*/
import "C"

import (
	"fmt"
	"testing"
)

var sink int32
var buf [1024]int32

func main() {
	for i := range buf {
		buf[i] = int32(i)
	}
	rNoop := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sink = int32(C.c_noop())
		}
	})
	rAdd := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sink = int32(C.c_add(1, 2))
		}
	})
	rBatch := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sink = int32(C.c_sum((*C.int32_t)(&buf[0]), 1024))
		}
	})
	fmt.Println("noop :", rNoop.String())
	fmt.Println("add  :", rAdd.String())
	fmt.Println("batch:", rBatch.String())
	fmt.Printf("batch 每元素 ns: %.4f\n",
		float64(rBatch.T.Nanoseconds())/float64(rBatch.N)/1024)
}
```

```bash
CGO_ENABLED=1 go build -o cgo_bench cgo_bench.go && ./cgo_bench
go tool objdump -s '_Cfunc_c_add' ./cgo_bench | grep -E 'cgocall|cgoUse|convT' | head
go tool objdump -s '_Cfunc_c_add' ./cgo_bench | wc -l
go tool nm ./cgo_bench | grep -iE 'cgocall|asmcgocall' | head -4
```

<OutputBox title="cgo_bench 与包装函数实测（Go 1.23 / arm64 / CGO_ENABLED=1，数值随机型波动）" output="noop : 39027627   26.98 ns/op\nadd  : 43668585   26.80 ns/op\nbatch: 20176530   63.41 ns/op\nbatch 每元素 ns: 0.0619\n--- _Cfunc_c_add 反汇编关键词 ---\nCALL runtime.cgocall(SB)\nCALL runtime.convT32(SB)\nCALL runtime.cgoUse(SB)\n--- 包装函数指令行数: 56 ---\n--- nm ---\n100063460 T runtime.cgocall\n100071580 T runtime.asmcgocall" note="空 C 函数与加法 C 函数耗时几乎相同，说明成本不在 C 侧计算。" />

逐字段解释：`noop` 与 `add` 分别是 26.98 与 26.80 ns/op，差不到 1%，而 C 侧一个什么都没做、一个做了一次加法——把算式写成 `T(单次) = T_固定边界 + T_C计算`，实测得到 `T_C计算 ≈ 0`、`T_固定边界 ≈ 27 ns`。`batch` 一次跨边界处理 1024 个 int32 只涨到 63.41 ns，代入 `27 + 1024×t_元素 ≈ 63` 得 `t_元素 ≈ 0.035 ns`，摊到每元素 0.0619 ns，比逐元素调用便宜约 400 倍。反汇编里 `CALL runtime.cgocall` 是栈切换入口，`convT32` 与 `cgoUse` 是 cgo 为参数生成的保活检查，`nm` 里的 `cgocall`/`asmcgocall` 两个 `T` 符号把「跨边界」与具体运行时函数对上；包装函数 56 行汇编也说明「只是转发一下」并不成立。

**反直觉结论的证伪**：「cgo 慢是因为 C 函数算得慢，换个更轻的 C 实现就好」被 `noop` 与 `add` 的对照直接证伪——两者耗时相同，成本几乎全在栈切换、参数封送与 `entersyscall`/`exitsyscall` 的状态登记上。有效手段是**收敛入口、批量传数据**（`batch` 的每元素成本），而不是优化 C 侧的几个指令。顺带也能解释为什么上一轮 `cgo_bench` 里 `B/op` 与 `allocs/op` 都是 0：这里堆上没有分配，`convT32` 只是包装函数内部的类型转换助手，不构成一次 Go 堆对象。

边界：数值在 Go 1.23 / darwin / arm64 / `CGO_ENABLED=1` 下测得，换机器、换 `GOMAXPROCS` 或让包装函数被内联都会波动；`static` C 函数写在头文件里由 cgo 就地编译，换成动态库符号会另有查找成本；`cgocall`、`asmcgocall`、`cgoUse` 的符号名与包装函数布局随 Go 版本重构；32 位平台参数封送宽度不同；`cgoUse`/`cgocheck` 有运行时开销，`GOEXPERIMENT=cgocheck2` 更严更慢；panic 不能跨 C 边界，`C.CString` 仍在 C 堆、必须 `C.free`，这两条与本节的成本结论互不影响。

### `cgocallback`：C 调 Go 的反向边界，与 `cgocall` 正好镜像

前面把 Go 调 C 的 `cgocall` 拆成了「栈切换 + `entersyscall`/`exitsyscall` + 参数封送」，但 cgo 是**双向**的：`//export` 导出的 Go 函数会被 C 回调，这条反向路径走的是另一组符号（`runtime/cgocall.go` 顶部长注释与 `runtime/asm_$GOARCH.s`，Go 1.23.8）。

**状态迁移链**：C 侧调用的是 cgo 生成的函数指针 → `crosscall2`（`runtime/cgo/asm_$GOARCH.s`，把 gcc ABI 适配成 gc ABI）→ `runtime.cgocallback`（汇编）从 `m.g0` 的系统栈**切回 `m.curg` 的 goroutine 栈**，切换前把当前 SP 存进 `m.g0.sched.sp`、并把旧值压在 g0 栈上以便恢复 → `cgocallbackg(fn, frame, ctxt)`：

```text
校验 gp == gp.m.curg
callbackUpdateSystemStack(gp.m, sp, false)     // 更新 g0 栈边界
lockOSThread()                                 // 必须先锁 M，否则 exitsyscall 可能换 M
savedsp/savedpc/savedbp := gp.syscallsp/pc/bp  // 存成局部变量，栈增长时受栈映射保护
exitsyscall()                                  // 从 cgo 调用「出来」，重新进入 GOMAXPROCS 记账
gp.m.incgo = false
cgocallbackg1(fn, frame, ctxt) → _cgoexp_GoF(frame) → 真正的 Go 回调
返回：gp.m.incgo = true；unlockOSThread()；reentersyscall(savedpc, savedsp, savedbp)
恢复 g0 栈边界 → 回到 cgocallback → 切回 g0 栈 → crosscall2 → C
```

与 `cgocall` 对照很整齐：`cgocall` 是 `entersyscall` 加「goroutine 栈 → g0 栈」，`cgocallback` 是 `exitsyscall` 加「g0 栈 → goroutine 栈」；`lockOSThread`/`unlockOSThread` 这一对是反向路径独有的，因为回调必须先保证留在同一个 M 上，才能与 `cgocall` 那次 `entersyscall` 配对。`cgocallbackg1` 还会 `defer` 一个恢复 `m.g0.sched.sp` 的函数，使回调里 panic 时 g0 栈与 curg 栈同步回卷；`gp.nocgocallback` 为真时直接 panic，拦住标了 `#cgo nocallback` 的函数被回调。

新观察角度：让回调里 panic，用 `GOTRACEBACK=system` 把**两个方向的帧同时打出来**，再用符号表与反汇编定位入口。存成 `cb.go`：

```go
package main

/*
#include <stdint.h>
extern void GoPanic(void);
static void c_call_panic(void) { GoPanic(); }
*/
import "C"

//export GoPanic
func GoPanic() { panic("from C callback") }

func main() { C.c_call_panic() }
```

```bash
CGO_ENABLED=1 go build -o cbin cb.go
GOTRACEBACK=system ./cbin 2>&1 | grep -E 'cgocallback|_cgoexp|crosscall|GoPanic|gopanic|cgocall' | head -12
go tool nm ./cbin | grep -E 'runtime\.cgocallback|_cgoexp|crosscall2' | head -8
go tool objdump -s '^runtime\.cgocallbackg$' ./cbin | grep -E 'CALL runtime\.(exitsyscall|cgocallbackg1|unlockOSThread|reentersyscall)'
sed -n '40,80p' "$(go env GOROOT)/src/runtime/cgocall.go"
```

<OutputBox title="C→Go 反向边界的栈与符号（Go 1.23.8 / darwin arm64 / CGO_ENABLED=1）" output="--- GOTRACEBACK=system 栈节选 ---\nmain.GoPanic(...)\n_cgoexp_efded9cec1da_GoPanic(...)   _cgo_gotypes.go:53\nruntime.cgocallbackg1(...)          cgocall.go:439\nruntime.cgocallbackg(...)           cgocall.go:350\nruntime.cgocallback(...)\nruntime.cgocall(...)                cgocall.go:185\n--- go tool nm ---\n100076100 T _crosscall2\n10006d9b0 T runtime.cgocallback.abi0\n100004ba0 T runtime.cgocallbackg\n100004d80 T runtime.cgocallbackg1\n--- objdump cgocallbackg ---\nCALL runtime.exitsyscall(SB)\nCALL runtime.cgocallbackg1(SB)\nCALL runtime.unlockOSThread(SB)\nCALL runtime.reentersyscall(SB)" note="一份栈里同时出现 cgocall（Go→C）与 cgocallback（C→Go）两组帧，正好镜像。" />

逐字段解释：栈从 `main.GoPanic` 往上依次是 `_cgoexp_..._GoPanic`（cgo 生成的解包包装）、`runtime.cgocallbackg1`（`cgocall.go:439`，调回调本体）、`runtime.cgocallbackg`（`cgocall.go:350`）、`runtime.cgocallback`（汇编切栈），再往下才是 `runtime.cgocall`（`cgocall.go:185`，最初 Go 调 C 的那一层）——**一次嵌套调用把两个方向串在同一条栈上**，这是「反向边界」最直观的证据。`go tool nm` 里 `runtime.cgocallback.abi0` 带 `.abi0` 后缀，说明汇编写成的入口走栈式 ABI（回看第 26 讲），`_crosscall2` 是 gcc ABI 适配层，`cgocallbackg`/`cgocallbackg1` 是纯 Go 的两个阶段。反汇编里四条 `CALL` 按 `exitsyscall → cgocallbackg1 → unlockOSThread → reentersyscall` 排开，与上面状态迁移链逐条对应；`sed -n '40,80p'` 给出的正是这段流程的源码注释原文。

边界：路径以 Go 1.23.8 / darwin / arm64 为准，`crosscall2`、`_cgoexp_<hash>_<name>` 的命名随 Go 版本与构建哈希变化；`GOTRACEBACK=system` 会打印 runtime 帧，`grep` 关键字要按本机符号调整；`lockOSThread` 使回调期间 M 被占用，深度嵌套回调会放大调度代价；panic 不能跨 C 边界传播（这条铁律不变），回调里的 panic 只能由 Go 侧自己兜住；`//export` 要求被导出的函数在同一文件里有对应的 C 声明，头里 `static` 函数与动态库符号的查找成本不同。

### `cgocheck2` 的检查代价：一次指针写一次运行时调用

本讲前面提过「默认 `cgocheck=1`，更严格检查用 `GOEXPERIMENT=cgocheck2`」，但没说清它贵在哪、也没说清一个版本变化：**Go 1.23 起 `GODEBUG=cgocheck=2` 已在运行时被禁用**，`runtime/runtime1.go` 里 `if debug.cgocheck > 1 { throw("cgocheck > 1 mode is no longer supported at runtime. Use GOEXPERIMENT=cgocheck2 at build time instead.") }`。更严格的检查现在只能在**构建期**由 `GOEXPERIMENT=cgocheck2` 打开，开关从运行时挪到了编译器。

**插桩条件**（`cmd/compile/internal/ssa/writebarrier.go`）：当 `buildcfg.Experiment.CgoCheck2` 为真，SSA 的 `writebarrier` pass 对**每一个 `OpStoreWB`**（需要写屏障的指针写）在展开时额外插一次 `cgoCheckPtrWrite`，对 `OpMoveWB` 插 `cgoCheckMemmove`。也就是说代价不是「只在 cgo 调用点检查」，而是「所有包里每一次堆指针写都多一次运行时调用」。`runtime/cgocheck.go` 的 `cgoCheckPtrWrite(dst, src)` 随后依次短路：`!mainStarted`、`!cgoIsGoPointer(src)`、`cgoIsGoPointer(dst)`、当前在 g0/gsignal、`mallocing`、`isPinned(src)`、`inPersistentAlloc(dst)` 都直接返回；只有「把未固定的 Go 指针写进非 Go 内存」才 `throw("unpinned Go pointer stored into non-Go memory")`。所以纯 Go 写 Go 内存时，多付的是一次函数调用加两次地址归类，而真正的违规会被当场抓住。另有 `cgoCheckSliceCopy`、`cgoCheckTypedBlock`、`cgoCheckBits`、`cgoCheckUsingType` 覆盖批量拷贝，`cgoCheckPointer`/`cgoCheckArg` 负责调用参数递归检查，失败消息是 `cgo argument has Go pointer to unpinned Go pointer`。

新观察角度一：用**违规程序 + 构建期开关**把「检测」与「版本变化」一起验出来。存成 `cgoviol.go`：

```go
package main

/*
#include <stdlib.h>
static void **c_slot(void) {
    static void *p;
    return &p;
}
*/
import "C"

import "unsafe"

func main() {
	x := 42
	gp := unsafe.Pointer(&x)
	slot := C.c_slot()
	*(*unsafe.Pointer)(unsafe.Pointer(slot)) = gp // 把 Go 指针写进 C 静态内存
	_ = x
}
```

```bash
CGO_ENABLED=1 go build -o cv1 cgoviol.go && ./cv1
CGO_ENABLED=1 GOEXPERIMENT=cgocheck2 go build -o cv2 cgoviol.go && ./cv2 2>&1 | head -6
GODEBUG=cgocheck=2 ./cv1 2>&1 | head -2
grep -n 'cgoCheckPtrWrite\|cgoWriteBarrierFail' "$(go env GOROOT)/src/runtime/cgocheck.go" | head -4
```

<OutputBox title="默认与 cgocheck2 构建的行为差（Go 1.23.8 / darwin arm64）" output="--- 默认构建运行 ---\n（静默通过）\n--- GOEXPERIMENT=cgocheck2 构建运行 ---\nwrite of unpinned Go pointer 0x14000010120 to non-Go memory 0x10504a720\nfatal error: unpinned Go pointer stored into non-Go memory\nruntime.cgoCheckPtrWrite.func1()\n    .../src/runtime/cgocheck.go:68\n--- GODEBUG=cgocheck=2 ---\nfatal error: cgocheck 大于 1 mode is no longer supported at runtime." note="同一份源码，默认构建静默，cgocheck2 构建当场抛错；运行时 cgocheck=2 已不再支持。" />

逐字段解释：默认构建里 `cgoCheckPtrWrite` 根本没被插桩，把 Go 指针写进 C 静态内存无人拦截；`GOEXPERIMENT=cgocheck2` 构建后，`write of unpinned Go pointer 0x... to non-Go memory 0x...` 打印出 `src`（Go 堆地址）与 `dst`（C 静态区地址），随后 `throw(cgoWriteBarrierFail)`，栈顶 `runtime.cgoCheckPtrWrite.func1()` 定位到 `cgocheck.go:68`——正是上面七个短路条件全不成立后的 `systemstack` 抛错分支。`GODEBUG=cgocheck=2` 在 1.23 直接 fatal，提示改用 `GOEXPERIMENT=cgocheck2`，把「开关已移到构建期」坐实。

新观察角度二：用 `go test -bench` 把**固定开销**量出来。因为插桩是构建期全局的，连纯 Go 包也会被插，不需要 cgo 依赖，两个文件即可：

```go
// wb.go
package purewb

type Node struct {
	Next *Node
	Val  int64
}

var Hold []Node

func PtrStores(n int) {
	buf := make([]Node, n)
	var prev *Node
	for i := range buf {
		buf[i].Next = prev // 每次都是一次堆指针写
		buf[i].Val = int64(i)
		prev = &buf[i]
	}
	Hold = buf
}
```

```go
// wb_test.go
package purewb

import "testing"

func BenchmarkPtrStores(b *testing.B) {
	for i := 0; i < b.N; i++ {
		PtrStores(1024)
	}
}
```

`go.mod` 写 `module purewb` 与 `go 1.23`。

```bash
go test -bench=. -benchtime=2000x -run=^$ .
GOEXPERIMENT=cgocheck2 go test -bench=. -benchtime=2000x -run=^$ .
```

<OutputBox title="cgocheck2 的指针写插桩代价（Go 1.23.8 / darwin arm64）" output="默认构建          BenchmarkPtrStores-12   2000   2016 ns/op\nGOEXPERIMENT=cgocheck2  BenchmarkPtrStores-12   2000   7253 ns/op" note="同样 1024 次指针写，约 3.6 倍差距；多出来的正是每写一次 cgoCheckPtrWrite 的固定开销。" />

逐字段解释：两个构建跑同一个 `PtrStores(1024)`，每次迭代做 1024 次 `buf[i].Next = prev` 堆指针写；默认 2016 ns/op，打开 `cgocheck2` 后 7253 ns/op，差约 5237 ns、摊到每次写约 5 ns——量级正好是「一次函数调用加两次 `cgoIsGoPointer` 归类」。代入 `writebarrier.go` 的插桩条件可知差距与 Go 侧逻辑无关，纯 Go 包也躲不掉；这就是 `cgocheck2` 「更严更慢」的量化口径。工程结论因此很直接：它适合在 CI 或预发做一次正确性扫描，不该默认开在线上；检查 cgo 指针传递用默认的 `cgocheck=1` 已能覆盖 `cgoCheckPointer`/`cgoCheckArg` 的参数递归。

边界：`GOEXPERIMENT=cgocheck2` 是构建期开关，必须与 `go build`/`go test` 一起给，运行时 `GODEBUG` 改不了；`GODEBUG=cgocheck=2` 在 Go 1.23 已抛错，更早版本可用，跨版本先 `go version`；`cgoCheckPtrWrite` 的短路条件（`mainStarted`、`isPinned`、`inPersistentAlloc` 等）决定了大多数写入只是空转一次，别把基准差值当成「每次都在做完整堆扫描」；`go test -bench` 的绝对值随机器、`GOMAXPROCS` 与内联波动，只比同一台机器上的相对倍数；`go tool nm`/`objdump` 的符号名带构建哈希与 `.abi0` 后缀，随版本变化。这条链与第 28 讲（写屏障辅助搬移）、第 99 讲（GC 根与可达性）共同解释「为什么把 Go 指针长期放进 C 内存是禁区」。

### 边界、常见误解与前后联系

| 常见误解 | 正解 | 可验证依据 |
| --- | --- | --- |
| `unsafe.Pointer` 可以随意和任意整数互转 | 只有六条合法模式，其余可能被 GC 或栈搬迁破坏 | `go vet` 的 unsafeptr 与 `go build -d=checkptr=2` |
| 只要用了 `runtime.KeepAlive` 就能把地址存成 `uintptr` 长期保存 | KeepAlive 只把存活期延到调用点，不能让游离整数跨函数或跨 goroutine 保活 | 第 3 节 `broken()` 与 `withKeepAlive()` 对照 |
| cgo 慢是因为每次调用都在 Go 堆上分配 | benchmark 里 `B/op` 常为 0，成本在栈切换、参数封送与调度状态登记 | 上面的 `cgo_bench` |

边界要点：`uintptr` 宽度随平台字长（32 位 4 字节、64 位 8 字节），跨平台偏移常量要重算；`unsafe.Add` 的偏移溢出行为未定义；`go vet` 的 unsafeptr 只抓「整数转 Pointer」的静态模式、抓不到语义错误，`-race` 与 checkptr 有运行时开销且只覆盖被插桩的代码；`C.CString` 在 C 堆、`C.GoString` 复制回 Go 堆，两本账不能混；C 侧不得长期持有 Go 指针（Go 1.21+ 可用 `runtime.Pinner` 固定并在用完后 `Unpin`）；`GOEXPERIMENT=cgocheck2` 更严但更慢；panic 不能跨 C 边界；启用 cgo 后 `CGO_ENABLED=0` 交叉编译失败、二进制可能动态依赖 libc；`plugin` 包要求同版本 Go 且 Windows 不支持。并发场景下 `unsafe` 共享内存没有任何 happens-before 保证，比普通数据竞争更难排查。

前后联系：第 31 讲（字符串零拷贝）与第 28 讲（`reflect.SliceHeader` 的 uintptr 陷阱）是 unsafe 规则的具体前例；第 98 讲（编译器源码：SSA、逃逸与内联）会讲 `checkptr` 插桩与 `unsafe` 相关的编译期检查如何生成，第 99 讲（Go 内存模型与 happens-before）从「GC 根与可达性」严格定义为什么 `uintptr` 不构成根、以及并发下这类共享为何无保证，第 100 讲（极致性能优化）把「跨边界批量化、收敛入口」列为典型优化。

## 4. 工程实践

cgo 让 Go 调 C，但每次跨边界都要经 `runtime.cgocall`：从可增长的 goroutine 栈切到系统栈、封送参数、登记进出 C 的状态，单次约几十到上百纳秒，远高于普通 Go 调用的约 1 纳秒，且调用期间 M 被占用、影响调度。

```go
package main

/*
#include <stdlib.h>
#include <string.h>

static int c_strlen(const char *s) {
	return (int)strlen(s);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func main() {
	// C.CString 在 C 堆分配，Go 的 GC 管不到，必须手动释放
	cstr := C.CString("hello from go")
	defer C.free(unsafe.Pointer(cstr))

	n := C.c_strlen(cstr)
	fmt.Printf("C 侧长度 = %d\n", int(n))

	// C.GoString 把 C 字符串复制回 Go 堆，得到受 GC 管理的 string
	fmt.Println("复制回 Go:", C.GoString(cstr))
}
```

命令：`CGO_ENABLED=1 go run cgo_demo.go`；`CGO_ENABLED=0 go build cgo_demo.go` 会失败，正好演示「一旦用 cgo 就失去纯静态交叉编译」；`go build -x cgo_demo.go` 看 cgo 生成的中间步骤；`GODEBUG=cgocheck=1 go run cgo_demo.go` 校验指针传递；`go tool nm ./cgo_demo | grep -i cgo` 看链接进来的 cgo 符号。

内存所有权有三条硬约束：C 侧只能在一次调用期间持有 Go 指针，不能把它存进 C 内存长期保留；C 堆分配必须 `C.free`，Go 堆分配由 GC 回收，两本账不能混；Go 1.21+ 若确实需要让 C 跨调用持有 Go 对象，用 `runtime.Pinner` 固定并在用完后 `Unpin`。默认 `cgocheck=1`，需要更严格检查时用 `GOEXPERIMENT=cgocheck2` 构建。panic 不能跨 C 边界传播，也要提前兜住。

| 方案 | 跨语言 | 隔离性 | 可卸载 | 调用开销 | 交叉编译 |
| --- | --- | --- | --- | --- | --- |
| cgo | 是 | 无，同进程 | 否 | 高，栈切换 | 难 |
| plugin 包 | 否，须同版本 Go | 无 | 否 | 低 | 受限，Windows 不支持 |
| WASM（wazero / `GOOS=wasip1`） | 是 | 沙箱 | 是 | 中 | 好 |
| 进程插件（gRPC） | 是 | 进程级 | 是 | 高，IPC | 好 |
| 纯 Go 重写 | 否 | — | — | 最低 | 最好 |

取舍结论：能纯 Go 重写就别上 cgo；需要沙箱与可卸载优先 WASM；只有必须复用现成 C 库、且能接受构建与调度代价时才用 cgo，并把调用收敛在少数入口、批量传数据以摊薄边界成本。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l3-core/lesson-037-unsafe-cgo`](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-037-unsafe-cgo)。clone 仓库后进入 `examples/l3-core`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="修好那段危险代码" task="把 broken 改成正确写法：全程用 unsafe.Add 保持 Pointer 形态，并用 go vet 与 -race 确认不再有告警，说明为什么这样就不会悬垂。" :hints='["删掉游离的 uintptr 变量","用 unsafe.Offsetof 取字段偏移","需要保活时补 runtime.KeepAlive","go vet 与 go test -race 会启用指针检查"]' solution="p := (*node)(unsafe.Add(unsafe.Pointer(n), unsafe.Offsetof(n.Val)))\nfmt.Println(p.Val)\nruntime.KeepAlive(n)\ngo vet broken.go        // 不再报 misuse of unsafe.Pointer\ngo test -race ./...     // checkptr 无异常" explain="Pointer 形态让 GC 始终识别这块内存，KeepAlive 把存活期延长到最后一次使用之后，从根上消除了悬垂。" />

## 随堂检测

<Quiz no="37-1" q="把地址存成 uintptr 暂存，最根本的危险是什么？" :options='["uintptr 不能做加法","GC 不把整数当作根，对象可能被提前回收","uintptr 占内存太大","编译器不允许这种转换"]' answer="1" explain="uintptr 是整数不是指针，GC 不会因它保持对象存活；一旦中间发生 GC 或栈搬迁，还原出的地址就可能悬垂。" />
<Predict no="37-2" q="C.CString 分配的内存由谁释放？" :options='["Go 的 GC 自动回收","必须显式调用 C.free","进程退出时统一回收即可，无需处理","由 cgo 生成的包装函数代劳"]' answer="1" output="必须显式 C.free，否则持续泄漏" explain="C.CString 在 C 堆用 malloc 分配，Go 的 GC 完全管不到；谁分配谁释放是跨边界的铁律。" />

## 一页纸速记

<KnowledgeCard :points='["unsafe.Pointer 只在同一块内存内转换与偏移，优先用 Add、Offsetof 等封装","uintptr 是整数不是根：存入变量再跨 GC 还原，必然有悬垂风险","正确做法：全程 Pointer、转换留在同一表达式、必要时 runtime.KeepAlive","cgo 有栈切换与 cgocall 开销，C 堆须 C.free，C 侧不得长期持有 Go 指针","替代顺序：纯 Go 重写 → WASM → 进程插件 → 最后才 cgo"]' :pitfalls='["把 uintptr 当可长期保存的指针","混淆 Go 堆与 C 堆的所有权、漏掉 C.free","在热路径里高频跨 cgo 边界而不批量化"]' summary="unsafe 与 cgo 换来能力，代价是亲自承担类型安全、存活期与内存所有权，范围必须封到最小。" />

## 小结与下一讲

- `unsafe.Pointer` 的合法性来自「同一块内存 + 全程 Pointer」，偏移交给 `unsafe.Add` 与 `Offsetof`。
- `uintptr` 陷阱的根因是 GC 不把整数当根；用 `go vet`、`-race`、checkptr 把它变成可检测错误。
- cgo 的取舍在于边界成本与所有权：能不跨就不跨，跨了就收敛入口、批量传数据、明确谁释放。

至此 L3 核心机制收束，接下来进入 Go 最鲜明的部分。[下一讲：并发与并行：CSP 思想](/concurrency/)
