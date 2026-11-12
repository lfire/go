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
