---
title: 第 27 讲 · 逃逸分析与编译器决策
description: 看懂 -gcflags=-m 并写出少分配的代码：栈分配与逃逸、常见场景、指针返回与减少堆分配的实践
---

<StageHeader stage="L3 · 核心机制" level="★★★★☆" time="40 分钟" goal="看懂 -gcflags -m 并写出少分配的代码" prereq="第 26 讲 · 值语义深潜：赋值、传参、拷贝成本" />

# 第 27 讲 · 逃逸分析与编译器决策

> 上一讲留了个尾巴：取地址可能让对象「搬到堆」。可变量到底住栈还是住堆，从来不由 `new` 或你写不写 `&` 决定，而是编译器在编译期做的一次推断——**逃逸分析**。读懂它，你才能解释「为什么这段代码每次调用都分配内存」「为什么接口传参比具体类型慢」，并用命令而不是猜测去优化。

## 这一讲你会得到什么

<CheckList id="goal-27" title="本讲目标" :items='["能解释栈分配与堆分配的成本差别","会用 go build -gcflags=-m 读懂逃逸决策","能列举并复现五类常见逃逸场景","掌握预分配、避免装箱等减少堆分配的做法"]' />

## 1. 先建立直觉

把函数栈帧想成随身携带的便签本：调用开始撕一页，返回时整页丢掉，零回收成本。堆则是公共公告板：谁都能贴，但要靠 GC 定期打扫，分配和回收都有代价。编译器像一位管理员，逐条检查「这张便签上的地址，会不会被带出房间」。只要指针的生命周期可能长于函数，就判定**逃逸**，把它改贴到公告板上。

<ConceptDiagram type="escape" title="逃逸分析：指针是否活过函数" caption="指针留在函数内 → 栈；被返回、被闭包捕获或装箱外泄 → 堆" />

注意这是**编译期**的保守推断：编译器只在能证明安全时留在栈上，拿不准就一律搬到堆。

## 2. 语法与规则

先对比「返回值」和「返回指针」这两个只差一个符号的函数。

```go
package main

type User struct {
	ID   int
	Name string
}

//go:noinline
func stackUser() User { // 返回的是值拷贝，不因「返回」而逃逸
	u := User{ID: 1, Name: "a"}
	return u
}

//go:noinline
func heapUser() *User { // 返回局部变量地址，u 必须逃逸
	u := User{ID: 2, Name: "b"}
	return &u
}

func main() {
	a := stackUser()
	b := heapUser()
	// 刻意不把结果传给 fmt.Println：...any 会让参数逃逸，干扰观察
	_ = a
	_ = b
}
```

真正的依据不是运行结果，而是编译器的决策日志。注意 `main` 里刻意没有把返回值交给 `fmt.Println`——它的 `...any` 参数本身会让实参逃逸，那样会污染这份日志：

```bash
# -m 打印逃逸决策，-m=2 更详细；grep 只保留与逃逸相关的行
go build -gcflags=-m=2 main.go 2>&1 | grep -E "escapes|moved to heap|leaking"
# 只想看单个文件，也可以直接用编译器
go tool compile -m main.go
```

<OutputBox title="逃逸分析输出（Go 1.23 实测，措辞随版本变化）" output="./main.go:16:2: u escapes to heap:\n./main.go:16:2: moved to heap: u" />

<LineByLine code="./main.go:16:2: u escapes to heap:\n./main.go:16:2: moved to heap: u\nstackUser 的 u 没有出现，说明它留在栈上" :notes='["escapes to heap 是 -m=2 给出的逃逸原因，定位到第 16 行的 u","moved to heap 是最终决策：这个变量的分配改到堆","报告里没有 stackUser 的 u，说明返回值拷贝不需要堆分配"]' />

把常见触发点放在一起，配上 `-gcflags=-m` 就能逐条复现：

```go
package main

import "fmt"

func makeSlice(n int) []byte {
	return make([]byte, n) // 切片要活着离开函数，底层数组在堆上
}

func main() {
	// 1) 被闭包捕获：x 要在函数返回后仍被访问
	x := 42
	f := func() int { return x }
	fmt.Println(f())

	// 2) 装箱进 interface{}：具体类型信息要随值一起存活
	var a any = 42
	fmt.Println(a)

	// 3) 传给 fmt 的 ...any 可变参数
	n := 7
	fmt.Println("n =", n)

	// 4) 动态长度且被返回的切片
	b := makeSlice(n)
	fmt.Println(len(b))
}
```

## 3. 深入一步

结论要有量化依据。`runtime.MemStats.Mallocs` 记录累计堆分配次数，对比两个函数各跑一千次，差异一目了然。

```go
package main

import (
	"fmt"
	"runtime"
)

//go:noinline
func noAlloc() int {
	x := [64]byte{} // 纯栈上，不逃逸
	return int(x[0])
}

//go:noinline
func withAlloc() *int {
	x := 1
	return &x // 逃逸，每次堆分配
}

func countAllocs(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := 0; i < 1000; i++ {
		f()
	}
	runtime.ReadMemStats(&after)
	return after.Mallocs - before.Mallocs
}

func main() {
	fmt.Println("noAlloc 1000 次 Mallocs:", countAllocs(func() { _ = noAlloc() }))
	fmt.Println("withAlloc 1000 次 Mallocs:", countAllocs(func() { _ = withAlloc() }))
}
```

| 场景 | 为什么判定逃逸 | 可验证依据 / 规避方向 |
| --- | --- | --- |
| 返回局部变量指针 | 函数返回后栈帧失效 | `-m` 报 `moved to heap`；改返回值或由调用方持有 |
| 被闭包捕获 | 闭包可能在函数返回后执行 | 只在闭包内用、或显式传值 |
| 装箱进 `any` / 接口 | 动态类型信息需与值同生命周期 | `leaking param`；热路径用具体类型或泛型 |
| 传给 `fmt` 的 `...any` | 参数被当作接口切片 | 日志热路径改用结构化、预格式化 |
| 动态大小且外泄的切片 | 编译期无法定长，又要返回 | `make` 时给足容量，减少扩容次数 |

<Term word="逃逸（escape）" text="指针的引用超出了创建它的函数作用域，编译器据此把本可栈分配的对象改到堆上，交给 GC 管理。" />

<Callout type="warn" title="-m 的输出不是稳定契约" text="不同 Go 版本措辞不同：escapes to heap、moved to heap、leaking param 指的是同一类决策；分析精度也随版本提升，同一段代码升级编译器后输出可能变化。另外「逃逸到堆」不必然是坏事，先 profile 再优化。" />

还要澄清一个流传很广的误解：**返回指针不比返回值慢，也不比它快，是两件事**。返回值可能触发大对象拷贝（见第 26 讲），返回指针则可能触发一次堆分配与后续 GC。哪个划算取决于对象大小、调用频率与是否真的需要共享，结论必须来自 `-gcflags=-m` 加基准测试，而不是口诀。

### 推导链：逃逸分析在编译器里怎么判定

结论的出处是 `cmd/compile/internal/escape/escape.go`：它遍历每个 `ir.Func`，在 IR 的赋值与取址节点上给变量打逃逸标签，标签沿返回、装箱、闭包捕获与接口传参传播，只要某条路径让指针的生命周期超出创建它的函数，就把该变量的分配点改写成堆分配（编译产物里对应 `runtime.newobject` 或 `mallocgc`）。这就是 `-gcflags=-m` 里那些措辞的真正来源，而不是运行时才做的判断。把第 2 节那段返回指针的程序存成 `esc.go`，可直接看到标签传播链：

```bash
go build -gcflags='-m -m' esc.go 2>&1 | grep -E 'escape|leaking|moved|flow'
go build -gcflags=-S -o /dev/null esc.go 2>&1 | grep -E 'newobject|convT'
# 若因构建缓存没有输出，先 go clean -cache 或追加 -a
```

<OutputBox title="-m=2 输出（Go 1.23，措辞随版本变化）" output="./esc.go:6:6: u escapes to heap:\n./esc.go:6:6:   flow: ~r0 = &u:\n./esc.go:6:6:     from &u (address-of) at ./esc.go:7:9\n./esc.go:6:6: moved to heap: u\n./esc.go:12:2: x does not escape\n./esc.go:18:13: ... argument does not escape" note="flow 行是标签传播路径，moved to heap 是最终决策；does not escape 的反例证明留在栈。" />

逐字段解释：`flow: ~r0 = &u` 说明返回值 `~r0` 直接引用了 `u` 的地址，`from &u (address-of)` 定位到取址语句，`moved to heap` 是最终把分配点改到堆，`x does not escape` 是同一份日志里的反例（该变量留在栈），`... argument does not escape` 说明编译器证明了某个可变参数未外泄。`go build -gcflags=-S` 的反汇编里同步出现 `CALL runtime.newobject`，把「决策」与「产物」对上。若能证明不外泄，同一行就会变成 `does not escape`，汇编里也没有 `newobject`。

**为什么这样设计**：栈分配随函数返回整帧回收，零 GC 成本；堆分配要进 `mallocgc`、占一个 size class 槽位、并在每轮 GC 里被扫描。若把「生命周期」判断放到运行时，就得为每个指针维护引用或存活元数据，写放大严重；若交给程序员显式指定（C 的 `malloc`/`free`、Rust 的所有权），精度高但心智负担与悬垂指针风险也高。Go 选「编译期数据流分析 + GC」自动兜底，代价是编译器**拿不准就一律搬堆**（保守推断），因此 `-m` 里常有意想不到的逃逸。两个极端都不可取：全部堆分配会让 `x := 1` 都触发分配器与 GC，吞吐崩溃；全部栈上则无法表达返回指针、闭包与接口，语言能力残缺。逃逸分析是两者之间唯一可行的自动折中，也解释了「为什么它必须是编译期的」。

### 标签的方向、内联改写与一次分配的真正有无

上一节把 `-m=2` 里的 `flow` 行当成「标签传播路径」读过，但没回答两个更细的问题：标签到底长什么样、为什么同一份日志里报了 `moved to heap`，实测却可能一次都不分配。先看数据结构。`cmd/compile/internal/escape` 给每个 `ir.Name` 建一个 `location`，核心是两组字段：`edges` 里每条边记录一个 `hole`（这个指针可能流向的目的地）与 `derefs`（经过几次解引用）。输入是 IR 上的赋值与取址节点：`&x` 生成一个指向 `x` 的 location，`y = &x` 把 `x` 的标签沿边复制到 `y` 的 location，反复迭代到不动点。最后凡是能沿某条边到达 `heap` 这个 hole 的 location，其分配点就被改写成 `runtime.newobject`；到不了的保持栈上。分支条件藏在 `derefs` 里：`derefs=0` 表示**指针本身**外泄（参数被原样存进返回值），`derefs` 为正表示**指针指向的内容**外泄（日志里写成 `leaking param content`），两者对调用方是否要跟着上堆的判定不同。

把三种参数命运放进同一个程序，`-m=2` 会给出方向各异的标签：

```go
package main

import (
	"fmt"
	"testing"
)

type Box struct{ P *int }

//go:noinline
func leakToResult(p *int) *Box { return &Box{P: p} } // 参数原样进返回值

//go:noinline
func readOnly(p *int) int { return *p } // 只读，不外泄

func newIntA() *int { x := 1; return &x } // 允许内联

//go:noinline
func newIntB() *int { x := 2; return &x } // 强制保留独立函数

func callerA() int { return *newIntA() }
func callerB() int { return *newIntB() }

var sink int

func main() {
	x := 1
	_ = leakToResult(&x)
	_ = readOnly(&x)
	a := testing.AllocsPerRun(1000, func() { sink = callerA() })
	b := testing.AllocsPerRun(1000, func() { sink = callerB() })
	fmt.Printf("callerA(newIntA 内联): %.0f allocs/op\n", a)
	fmt.Printf("callerB(newIntB 独立): %.0f allocs/op\n", b)
}
```

```bash
go run esc_dir.go
go build -gcflags=-m=2 -o /dev/null esc_dir.go 2>&1 | grep -E 'leaking param|does not escape|moved to heap|can inline'
```

<OutputBox title="实测与 -m=2 决策摘录（Go 1.23 / arm64）" output="callerA(newIntA 内联): 0 allocs/op\ncallerB(newIntB 独立): 1 allocs/op\n--- -m=2 决策摘录 ---\nleakToResult: parameter p leaks to ... with derefs=0\nleakToResult: leaking param: p\nreadOnly: p does not escape\nnewIntA: moved to heap: x\nnewIntB: moved to heap: x" note="同一个 -m=2 里既有 leaking param 也有 does not escape；两处 moved to heap 却对应 0 与 1 两种实测结果。" />

逐字段解释：`parameter p leaks to ... with derefs=0` 是标签从形参流向返回值存储的一条边，`derefs=0` 说明外泄的是指针本身；紧随的 `leaking param: p` 是同一条边的最终判定措辞；`readOnly` 打出 `p does not escape`，因为 `*p` 只读值、没有任何指向 `heap` 的边，参数与调用方的 `x` 都留在栈上；`main` 里的 `x` 则因被 `leakToResult(&x)` 传参而出现 `flow: {heap} = &x` 与 `moved to heap: x`，说明标签会**沿调用边反向传播到实参**，这是「取地址给函数也可能让调用方上堆」的依据。

**反直觉结论的证伪**：`-m=2` 里出现 `moved to heap: x` 并不等于这次调用一定发生堆分配。日志打印的是「函数被单独编译时的决策」，而内联发生在逃逸分析之前：`newIntA` 被内联进 `callerA` 后，`&x` 只在本帧内被立即解引用，不再有流向 `heap` 的边，分配点被改写回栈，实测 `0 allocs/op`；加上 `//go:noinline` 的 `newIntB` 无法内联，`x` 必须上堆，实测 `1 allocs/op`。所以「函数返回局部指针就一定分配」这句口诀，只在函数保持独立时成立；判断一次调用有没有分配，必须叠加调用点的 `-m=2` 诊断与 `testing.AllocsPerRun`，不能只看某一行 `moved to heap`。这也是第 36 讲内联预算、第 98 讲内联与逃逸顺序共同影响结论的地方：调预算、开 PGO、加 `-race` 改变函数形状，都可能让同一段代码的逃逸结论翻转。

边界：`leaking param` / `leaking param content` / `moved to heap` 都是 Go 1.23 的措辞，跨版本不稳定；`derefs` 的确切含义随编译器对间接赋值的建模微调；`AllocsPerRun` 只计堆分配，栈上大对象拷贝的成本要用 `go test -benchmem` 的 `ns/op` 与第 26 讲的 `unsafe.Sizeof` 一起看；结论与 `GOARCH` 无关（这是 IR 层分析），但内联决策随平台与版本变化，从而间接改变分配次数。

### `derefs` 的代数：为什么同一条日志会分出 `leaking param` 与 `leaking param content`

第二版已经把 `-m=2` 里的 `derefs=0` 读成「指针本身外泄」，但没回答这个数是怎么算出来的、也没有把 `leaking param content` 的判定分支写全。它其实是一条可以逐项代入的整数代数，全部落在 `cmd/compile/internal/escape` 的四个文件里。

**边权重定义**（`escape.go` 顶部长注释）：每个 IR 赋值被建模成一条 location 到 location 的有向边，权重 `derefs` 等于「解引用次数减取址次数」：

```text
p = &q     → -1     p = q    →  0
p = *q     →  1     p = **q  →  2
x.f = u[0] → 建模为 x = *u（切片下标隐含一次解引用，且不分字段、不分下标）
```

`&x` 本身不可取址，所以权重下界是 -1：`graph.go` 的 `hole.shift(delta)` 里 `if k.derefs < -1 { base.Fatalf("derefs underflow: ...") }` 把这条约束钉死。注意建模**不区分复合变量的成员**——`x.f` 与 `x.g`、`u[0]` 与 `u[1]` 被视为同一个 location，这正是逃逸分析保守性的一个具体来源。

**不动点传播**（`solve.go`）：从 `heap` 等 hole 反向做 LIFO 遍历，沿边时执行 `d := derefs + edge.derefs`（`solve.go:164`），并用 `if edge.src.walkgen != walkgen || edge.src.derefs > d` 决定要不要重新入队。也就是说，某个参数最终记到 `heap` 的 `derefs`，是所有可达路径里**权重和最小的那条**，而不是任意一条。

**标签编码与打印分支**（`leaks.go` 与 `escape.go`）：`leaks` 是 `[8]uint8`，`get(i) = int(l[i]) - 1`，因此存 0 表示「无流」即 -1；`add(i, derefs)` 只在 `derefs < old` 时覆盖，保存的正是最小权重；`set` 写 `v = derefs + 1`。最后 `reportLeaks` 的分支是：

```text
x := esc.Heap()
x 小于 0  → does not escape
x 等于 0  → leaking param: p          指针本身流入 heap
x 大于 0  → leaking param content: p  指针指向的内容流入 heap
```

于是「参数原样进全局」与「参数被解引用后其内容进全局」在日志里必然分道扬镳：`derefs=0` 报 `leaking param`，`derefs=1` 报 `leaking param content`。

新观察角度：不再读 `-m` 的文本，而是让编译器把优化诊断写成**结构化 JSON**（`-json=0,目录` 是 `go build -gcflags` 的内部开关），直接看 `code=leak` 里的 `derefs` 与 `escflow` 传播链。存成 `esc2.go`：

```go
package main

var global *int

//go:noinline
func leakContent(p **int) { global = *p } // 内容外泄，derefs=1

//go:noinline
func leakSelf(p *int) { global = p } // 指针本身外泄，derefs=0

func main() {
	x := 1
	p := &x
	leakContent(&p)
	leakSelf(p)
	_ = global
}
```

```bash
mkdir -p "$PWD/diag"
go build -gcflags="-json=0,$PWD/diag" -o /dev/null esc2.go
# 只抽 escape 包写出的 leak 诊断及其 escflow 解释链
python3 -c "
import json
for ln in open('diag/main/esc2.json'):
    o = json.loads(ln)
    if o.get('code') == 'leak':
        print(o['message'])
        for r in o.get('relatedInformation', []):
            print('   ', r['message'])
"
# 把判定分支的源码一起对上
sed -n '471,486p' "$(go env GOROOT)/src/cmd/compile/internal/escape/escape.go"
sed -n '63,75p' "$(go env GOROOT)/src/cmd/compile/internal/escape/leaks.go"
```

<OutputBox title="结构化诊断与源码分支（Go 1.23.8 / darwin arm64）" output="parameter p leaks to {heap} with derefs=1\n    escflow:    flow: {heap} = *p:\n    escflow:      from *p (indirection)\n    escflow:      from global = *p (assign)\nparameter p leaks to {heap} with derefs=0\n    escflow:    flow: {heap} = p:\n    escflow:      from global = p (assign)\n--- escape.go reportLeaks 分支 ---\nif x := esc.Heap(); x 不小于 0 {\n    if x == 0 { ... leaking param: %v ... }\n    else      { ... leaking param content: %v ... }\n}" note="同一份 JSON 里两条 leak 只差 derefs，措辞随之分成 leaking param 与 leaking param content。" />

逐字段解释：第一条 `derefs=1` 的 `flow: {heap} = *p` 与 `from *p (indirection)` 说明 heap 收到的是 `p` **指向的内容**（权重 -1 取址 + 2 解引用之类路径的净和），`reportLeaks` 走 `else` 分支打印 `leaking param content`；第二条 `derefs=0` 的 `flow: {heap} = p` 说明 heap 直接收到 `p` 本身，走 `x == 0` 分支打印 `leaking param`。`sed` 抽出的 `escape.go:471-486` 把这两个字符串的字面来源摆在眼前，`leaks.go:63-75` 给出 `get = int(l[i]) - 1` 与 `add` 取最小值的实现——所以「`derefs` 是最小权重和」不是推测，而是 `add` 里那行比较的直接结果。JSON 诊断的价值在于它把 `-m` 里散落在多行的 `flow` 收进一条 `relatedInformation`，且 `code`/`message` 是字段名而非自由文本，适合脚本核对。

边界：`-json=0,目录` 的输出格式是编译器内部版本 0，跨版本可能变；`escflow` 措辞、`leaks` 的 `[8]uint8` 与 `numEscResults` 都是 Go 1.23 的实现细节；`derefs` 上界被 `set` 截到 `math.MaxUint8`；建模不分字段与下标，因此「只让 `x.f` 逃逸」也可能把整个 `x` 判为外泄，这是精度限制而非 bug；`-gcflags` 默认只作用于当前包，看标准库要加 `all=`。这套代数在第 98 讲（编译器源码）会与 location 图的完整实现合流。

### PGO 怎样把逃逸结论翻过来：从内联闸门到不动点重算

「开 PGO 会改变逃逸」不是玄学，而是**编译主流程的先后顺序**决定的。看 `cmd/compile/internal/gc/main.go`：第 232 行 `interleaved.DevirtualizeAndInlinePackage(typecheck.Target, profile)` 先跑（内联与去虚拟化交织），第 259 行 `escape.Funcs(typecheck.Target.Funcs)` 才跑。内联在逃逸之前，意味着 PGO 改变了「哪些函数体被铺进调用点」，也就改变了逃逸分析看到的那张 IR 图；本讲前面 `newIntA` 被内联进 `callerA` 后 `0 allocs/op`、`newIntB` 被 `//go:noinline` 保留后 `1 allocs/op`，就是这条顺序的既有证据。

PGO 改的是内联闸门。`cmd/compile/internal/inline/inl.go` 里 `inlineMaxBudget = 80`、`inlineHotMaxBudget int32 = 2000`，`inlineBudget` 的算式是：

```text
budget := 80
若 IsPgoHotFunc(fn, profile) → budget = 2000，verbose 时打印 hot-node enabled increased budget=2000
若 relaxed                  → budget += inlheur.BudgetExpansion(80)
```

`inlheur.BudgetExpansion`（`inline/inlheur/scoring.go`）默认返回 `maxBudget`，即把 80 放宽到 160，供新内联器在调用点评分后「回头降级」用；`PGOInlinePrologue` 则按 profile 的调用边权重 CDF（默认阈值 99%）填 `candHotCalleeMap`，热点函数才拿到 2000。于是成本落在 80 到 2000 之间的函数，带不带 `default.pgo` 会得到**相反**的内联决策，而内联决策又直接决定逃逸标签是否沿调用边反向传播到实参（本讲 `main` 里 `x` 因 `leakToResult(&x)` 上堆那条链同理）。

新观察角度：用 `go test -cpuprofile` 生成 profile，再用 `-json` 结构化诊断对比 `-pgo=off` 与 `-pgo=default.pgo` 的**内联决策**，而不是再去看 `-m` 文本。目录中放三个文件：

```go
// lib.go
package pgofinal

// 30 条独立语句把成本推到 97：大于 80、小于 2000
func helper(x int) int {
	s := x
	s += 1
	s += 2
	s += 3
	s += 4
	s += 5
	s += 6
	s += 7
	s += 8
	s += 9
	s += 10
	s += 11
	s += 12
	s += 13
	s += 14
	s += 15
	s += 16
	s += 17
	s += 18
	s += 19
	s += 20
	s += 21
	s += 22
	s += 23
	s += 24
	s += 25
	s += 26
	s += 27
	s += 28
	s += 29
	s += 30
	return s
}

//go:noinline
func Hot(n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += helper(i)
	}
	return s
}
```

```go
// lib_test.go
package pgofinal

import "testing"

var Sink int

func BenchmarkHot(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Sink = Hot(100)
	}
}
```

`go.mod` 写 `module pgofinal` 与 `go 1.23`。注意 `lib.go` 里 30 条 `s += k` 要逐条写出（写成循环成本会低到落在 80 以内，闸门就不会翻转），`Hot` 上的 `//go:noinline` 保证 benchmark 采样落在 `helper` 这条热边上。

```bash
go test -bench=. -benchtime=300000x -cpuprofile=default.pgo -run=^$ .
mkdir -p "$PWD/off" "$PWD/on"
go build -pgo=off         -gcflags="-json=0,$PWD/off" -o /dev/null .
go build -pgo=default.pgo -gcflags="-d=pgodebug=1 -json=0,$PWD/on" -o /dev/null .
grep -h 'cost' off/pgofinal/lib.json on/pgofinal/lib.json
```

<OutputBox title="PGO 开关下的内联决策（Go 1.23.8 / darwin arm64）" output="--- -pgo=off ---\ncannotInlineFunction | function too complex: cost 97 exceeds budget 80\n--- -pgo=default.pgo ---\nhot-callsite-thres-from-CDF=33.33333333333333\nhot-node enabled increased budget=2000 for func=pgofinal.helper\ncanInlineFunction | cost: 97" note="同一个 helper：预算 80 时拒绝，预算 2000 时通过；闸门翻转发生在逃逸分析之前。" />

逐字段解释：`cost 97 exceeds budget 80` 是 `-pgo=off` 下 `CanInline` 用基准预算 80 的拒绝措辞；`-d=pgodebug=1` 打印的 `hot-node enabled increased budget=2000 for func=pgofinal.helper` 证明 profile 把 `helper` 标进 `candHotCalleeMap`、`inlineBudget` 换成 `inlineHotMaxBudget`；紧随的 `canInlineFunction | cost: 97` 是同一个函数在放大预算后的通过决策。`hot-callsite-thres-from-CDF` 是 `hotNodesFromCDF` 算出的热边权重阈值，随 profile 波动，不必当作固定值。把这条链与前面的主流程顺序合起来就是完整推导：profile → 热点集合 → `inlineBudget` 由 80 变 2000 → 内联闸门翻转 → `DevirtualizeAndInlinePackage` 改写 IR → `escape.Funcs` 在**新的** location 图上重算最小 `derefs` → 调用点处的 `moved to heap` 可能被改写回栈。需要说清的是：单独编译的 `helper` 自身仍会被打印一次自己的逃逸标签，翻转的是**调用点**的分配结果，判断一次具体调用有没有分配必须叠加调用点的诊断，不能只看函数单独编译时的那一行。

边界：`inlineMaxBudget=80` 与 `inlineHotMaxBudget=2000` 是 Go 1.23 的取值，历史上多次调整；`-d=pgodebug`、`-json=0`、`-pgo` 都是内部/版本相关开关；`default.pgo` 必须与构建的包路径匹配，profile 里没有热点边时不会放大预算；`-cpuprofile` 需要真实采样，空跑或 `-benchtime` 太小会生成无样本 profile；`interleaved.DevirtualizeAndInlinePackage` 是 Go 1.23 重构后的名字，旧版本流程不同；这条链在第 36 讲（内联预算与 PGO）与第 98 讲（编译器源码）会再展开。

### 边界、常见误解与前后联系

| 常见误解 | 正解 | 可验证依据 |
| --- | --- | --- |
| `new(T)` 一定在堆，`var x T` 一定在栈 | 去向由逃逸分析决定；`new` 只是返回指针的语法糖，若指针不外泄，结果同样可留在栈 | `go build -gcflags=-m` 里的 `new(T) does not escape` |
| 逃逸到堆一定更慢，要全部消灭 | 栈上大数组拷贝可能更贵；一次堆分配未必是瓶颈，先 profile 再动手 | `testing.AllocsPerRun` 与 `go test -benchmem` |
| 变量赋给接口或闭包就必然逃逸 | 若接口或闭包不离开当前函数、且编译器能证明，可能留在栈；但多数场景确实会漏 | `-m=2` 的 `flow` 传播路径 |

边界要点：跨包调用、接口方法、反射、`unsafe` 与 cgo 会让编译器丧失上下文，判定偏保守；`escapes to heap`、`moved to heap`、`leaking param` 是同一类决策的不同措辞，**不是跨版本稳定的契约**，分析精度还随 Go 版本提升（同一段代码升级后可能少逃逸）；这是编译期 IR 分析，本身与 `GOARCH` 无关，但内联、PGO 与 `-race` 会改变函数形状从而间接改变结论；观测局限在于 `-m` 只看决策、不看运行时代价，必须叠加 `testing.AllocsPerRun`、`go test -benchmem` 与 `GODEBUG=gctrace=1` 才能闭环。

前后联系：第 26 讲「取地址可能搬到堆」在这里得到机制解释，第 32 讲（分配器）接着讲堆分配具体落在哪个 size class；第 98 讲（编译器源码：SSA、逃逸与内联）会精读 `cmd/compile/internal/escape` 的数据流实现与内联如何改写逃逸结论，第 96 讲（GC 源码与真实调优案例）讲这些堆对象如何成为标记阶段的根与负担，第 100 讲（极致性能优化）把「先减分配次数」列为优化闭环第一环。

## 4. 工程实践

最常见的「隐形分配」是切片反复扩容。用 `testing.AllocsPerRun` 可以在普通 `main` 里直接量化预分配的收益。

```go
package main

import (
	"fmt"
	"testing"
)

//go:noinline
func noPrealloc(n int) []int {
	var s []int // nil slice，append 会多次扩容与堆分配
	for i := 0; i < n; i++ {
		s = append(s, i)
	}
	return s
}

//go:noinline
func prealloc(n int) []int {
	s := make([]int, 0, n) // 一次分配到位
	for i := 0; i < n; i++ {
		s = append(s, i)
	}
	return s
}

func main() {
	const n = 1024
	a := testing.AllocsPerRun(100, func() { _ = noPrealloc(n) })
	b := testing.AllocsPerRun(100, func() { _ = prealloc(n) })
	fmt.Printf("无预分配每次约 %.0f 次分配，预分配约 %.0f 次\n", a, b)
}
```

<Compare :panels='[{"label":"反复扩容","code":"var s []int; s = append(s, i)","note":"容量不足时反复分配新数组并拷贝旧元素"},{"label":"一次给足","code":"s := make([]int, 0, n)","note":"单次分配，append 不再触发扩容"}]' title="切片构建：扩容 vs 预分配" />

落地顺序建议是固定的三步：先用 `go build -gcflags=-m=2` 定位哪些变量在逃逸，再用 `go test -bench=. -benchmem` 看 `allocs/op` 有没有真的下降，最后才改代码。常见手段包括：`make` 给足容量、热路径避免 `fmt` 与 `...any` 装箱、能用具体类型或泛型就别用 `any`、把只需局部使用的指针关在函数内。对象复用还可以用 `sync.Pool`，但它和 GC 的关系要到阶段四才展开，别在这里急着套用。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l3-core/lesson-027-escape-analysis`](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-027-escape-analysis)。clone 仓库后进入 `examples/l3-core`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="找出逃逸点" task="写一个函数，把局部整型变量的地址存进 []*int 并返回该切片；用 go build -gcflags=-m 观察输出，指出哪一行报告了 escapes to heap 并解释原因。" :hints='["把地址放进会被返回的切片，等于让指针离开函数","-m 会同时打印内联与逃逸两类信息"]' solution="func collect() []*int {\n    x := 1\n    return []*int{&x}\n}" explain="x 的地址通过返回的切片外泄，编译器必须把 x 分配到堆；具体措辞以本机 -m 输出为准。" />

## 随堂检测

<Quiz no="27-1" q="func f() *int { x := 1; return &x } 里的 x 会分配到哪里？" :options='["一定在栈上","一定在堆上","由逃逸分析决定，这里会搬到堆","取决于调用方怎么用"]' answer="2" explain="x 的地址被返回，函数返回后栈帧失效，编译器必须让 x 逃逸到堆；用 go build -gcflags=-m 可看到 moved to heap。" />

<Predict no="27-2" q="给局部变量取地址并传给函数，一定会导致堆分配吗？" :options='["一定会","不一定，编译器看指针是否逃出函数","只有用 new 才会","只有全局变量才会"]' answer="1" output="不一定：若指针没有离开当前函数，仍可留在栈上" explain="逃逸分析看的是「指针生命周期是否超出函数」，不是「有没有取地址」。仅在函数内使用的指针会留在栈上。" />

<FillBlank no="27-3" tpl="查看逃逸分析的常用命令是 go build -gcflags=___ main.go；复用临时对象可以用 ___ 包里的 Pool。" :answers='["-m","sync"]' hint="填编译选项与标准库包名" explain="-m 打印优化与逃逸决策；sync.Pool 用于复用临时对象，它与 GC 的关系在阶段四展开。" :explains='["-m 让编译器把逃逸与内联决策打到标准错误，经 -gcflags 传入后可见 moved to heap 等判定，是观测栈堆分配的入口。","临时对象池由标准库 sync 包提供，按处理器分桶减少竞争，复用短命对象以降低分配与 GC 压力，但其中对象可能被 GC 清空。"]' />

## 一页纸速记

<KnowledgeCard :points='["变量住栈还是堆由编译期逃逸分析决定，与是否写 new 无关","常见逃逸：返回指针、闭包捕获、装箱进 any、传给 ...any、动态大小且外泄","go build -gcflags=-m=2 与 go test -benchmem 是定位与验证的两件工具","减少堆分配：预分配容量、避免热路径装箱、能把指针关在函数内就别外泄"]' :pitfalls='["以为返回指针一定更快，忽略堆分配与 GC 成本","把 -m 的输出措辞当成跨版本固定的契约","未经 profile 就盲目消灭所有逃逸"]' summary="逃逸分析是编译器的保守推断：指针活得比函数长，就搬到堆。" />

## 小结与下一讲

- 栈分配随函数返回自动回收，堆分配需要 GC；由编译期逃逸分析决定去向，不是 `new` 决定。
- `-gcflags=-m=2` 打印决策，措辞随版本变化；`AllocsPerRun` 与 `benchmem` 用来量化。
- 优化闭环是「定位 → 量化 → 改代码 → 复测」，预分配和少装箱是收益最稳的两种手段。

既然切片扩容既触发堆分配、又改变 `cap`，那它内部的三元组到底怎么排、扩容策略在不同 Go 版本又差多少？下一讲拆开切片看。

[下一讲：切片底层：三元组、扩容与共享陷阱](/core/slice-internals)
