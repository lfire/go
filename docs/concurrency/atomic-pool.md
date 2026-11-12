---
title: 第 046 讲 · atomic 与 sync.Pool
description: 理解原子操作、CAS、atomic.Value 与 sync.Pool 的对象复用，看清无锁与减少分配的收益、代价和 GC 清空语义
---

<StageHeader stage="L4 · 并发编程" level="★★★★☆" time="40 分钟" goal="理解无锁与对象复用的收益和代价" prereq="第 045 讲 · sync 原语" />

# 第 046 讲 · atomic 与 sync.Pool

> 锁能保护复杂临界区，但高竞争下有阻塞开销。若只保护一个整数、一份只读配置，或想复用高频创建的临时缓冲区，杀鸡用牛刀就可惜了。sync/atomic 用一条 CPU 指令完成读改写，sync.Pool 在 GC 夹缝里复用对象。本讲讲清它们为什么快、边界在哪，尤其是 Pool 会被 GC 清空。

## 这一讲你会得到什么
<CheckList id="lesson-46" title="本讲目标" :items='["用类型化 atomic 与 CAS 写无锁计数和条件更新","用 atomic.Value 做配置热更新","掌握 sync.Pool 的 Get、Put、New 生命周期","理解 GC 清空 Pool 的 victim 机制与误用"]' />

## 1. 先建立直觉
锁像「进房间反锁」，安全但要排队；原子操作像「自助柜台」，CPU 保证读改写一气呵成。sync.Pool 像「周转筐」，领空筐用完还回去，省得每次拆新纸箱；但保洁（GC）可能把筐全收走，别指望「上次放的这次还在」。两者共同气质：快，但语义弱，须自己守边界。

<ConceptDiagram type="allocator" title="分配、复用与回收" caption="临时对象从 Pool 取用再归还，减少分配；GC 到来时缓存可能整体清空，下次 Get 回退到 New" />

## 2. 语法与规则
先看类型化原子值与 CAS。Go 1.19 起 atomic.Int64 这类封装有明确类型，零值即可用；CompareAndSwap 是「乐观锁」基石，只有当前值仍等于期望值才更新。

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	var total atomic.Int64 // 零值即可用
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				total.Add(1) // 原子加，无需互斥锁
			}
		}()
	}
	wg.Wait()
	fmt.Println("total =", total.Load())

	var limit atomic.Int64
	limit.Store(3)
	ok := limit.CompareAndSwap(3, 5) // 当前是 3，成功
	fmt.Println("CAS 成功 =", ok, "新值 =", limit.Load())
	fail := limit.CompareAndSwap(3, 9) // 期望已不符，失败
	fmt.Println("CAS 失败 =", fail, "值仍为 =", limit.Load())
}
```

<OutputBox title="运行结果" output="total = 8000；CAS 成功 = true 新值 = 5；CAS 失败 = false 值仍为 = 5" note="原子加不丢更新；CAS 只在值未被别人改动过时成功" />

atomic.Value 适合「写极少、读极多」的配置热更新：整份配置一次 Store，读方 Load 后类型断言，读路径无锁。

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type Config struct {
	Level string
	Ratio int
}

func main() {
	var current atomic.Value
	current.Store(Config{Level: "info", Ratio: 10}) // 首次 Store 决定类型

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c := current.Load().(Config) // 读路径无锁
			fmt.Println("读者", id, c.Level, c.Ratio)
		}(i)
	}
	wg.Wait()
	current.Store(Config{Level: "debug", Ratio: 50}) // 整体替换
	fmt.Println("更新后：", current.Load().(Config).Level)
}
```

CAS 三步值得逐行看。

<LineByLine code="var n atomic.Int64\nn.Store(3)\nok := n.CompareAndSwap(3, 5)" :notes='["类型化原子整数零值可用，编译器保证内存对齐","Store 把值设为 3，是一次不可分割的原子写","CAS 仅当当前值仍是 3 才改成 5，返回是否成功"]' title="CAS 三步" />

## 3. 深入一步
sync.Pool 的生命周期是 Get 取、Put 还、池空时 New 兜底。最易误解的是：对象可能在任意一次 GC 被移除，别假定 Put 后下次 Get 拿到同一个。下面用两次 GC 演示清空。

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
)

type Buffer struct{ data []byte }

var pool = sync.Pool{
	New: func() any { // 池空时兜底创建
		fmt.Println("调用 New 新建 Buffer")
		return &Buffer{data: make([]byte, 0, 1024)}
	},
}

func main() {
	b := pool.Get().(*Buffer) // 第一次 Get 触发 New
	b.data = append(b.data, 1)
	pool.Put(b)

	b2 := pool.Get().(*Buffer) // 未 GC 通常拿到刚归还的
	fmt.Println("复用对象，长度 =", len(b2.data))
	b2.data = b2.data[:0] // 归还前必须重置
	pool.Put(b2)

	runtime.GC()   // 第一次 GC：本地缓存降为 victim，仍可能被命中
	runtime.GC()   // 第二次 GC：victim 才真正丢弃
	_ = pool.Get() // 池空，只能再调 New
	fmt.Println("两次 GC 后，Pool 已被清空")
}
```

四条技术路线对比：

| 手段 | 适合保护 | 并发保证 | 主要边界 |
| --- | --- | --- | --- |
| Mutex | 复杂多步临界区 | 阻塞互斥，语义最强 | 高竞争有调度开销 |
| atomic | 单个数、指针、标志 | 单指令读改写 | 只护一个变量 |
| atomic.Value | 读多写极少的整份配置 | 无锁读、原子替换 | Store 类型须一致，不存 nil |
| sync.Pool | 可重建的临时大对象 | 自带并发安全 | GC 会清空，不能当持久缓存 |

<Term word="CAS" text="Compare-And-Swap：仅当内存当前值等于期望值才写新值并返回成功，是构建无锁算法的基本原语" />

<Callout type="warn" title="Pool 会被 GC 清空" text="Pool 只是「减少分配压力」的周转区，不是缓存。每轮 GC 都可能移除对象：Go 用 victim 机制，首轮 GC 把本地缓存降级，次轮才真正丢弃。所以「Put 后必能 Get 回」是错的，New 须能独立造干净对象，别把数据库连接等带外部资源的对象放进去。" />

<Callout type="tip" title="归还前先重置" text="取出的对象可能带残留数据，Put 前把切片截零、字段清零，Put 后别再持有引用。encoding/json 与 fmt 正是这样复用编码缓冲。" />

### 再深一层：Pool 的三层结构、GC 清空与 -race 的随机丢弃

要解释「Pool 为什么不是缓存」，得看 `sync/pool.go` 的真实结构。每个 P 持有一个 `poolLocal`，里面是 `private any`（当前 P 独占、LIFO 复用）与 `shared poolChain`（无锁 SPMC 队列，可被其它 P 偷取）；`Get` 的顺序是「先拿 private，再从 shared 出队，再从别的 P 偷，最后回退 `New`」，而 `Put` 优先塞进 private、满了才进 shared。整份缓存还分两级：`p.local`（primary）与 `p.victim`。`poolCleanup()` 在每次 GC 开始、STW 期间被运行时调用：第一轮把 `oldPools` 里的 `victim` 清空、再把 `allPools` 的 `local` 降级为新的 `victim`；第二轮才把上一轮的 `victim` 真正丢弃。这正是本讲示例里要连调两次 `runtime.GC()` 才能看到对象被清空的原因——GC 并不直接清 private/shared，只是把它们降级，对象最多活过两轮 GC。所以「Put 后下次 Get 必拿到同一个」在内存模型与实现上都不成立，Pool 只是「减少分配压力」的周转区。

一个更隐蔽、能亲手复现的事实：`Put` 在竞态检测开启时会故意捣乱。源码里写着 `if race.Enabled { if runtime_randn(4) == 0 { return } }`——`go test -race` 下每次 `Put` 有 1/4 概率把对象直接丢掉，目的就是暴露那些错误依赖「Put 后必复用」的代码。我们实测把脏对象 `Put` 回去再 `Get`，即便锁线程、`GOMAXPROCS(1)`，取回的仍是全新对象。因此本讲的 `TestPoolReset` 不赌取用顺序，而是直接构造一个「带上次残留」的脏 `*Buf` 来验证：`fillNoReset` 未截零就 append，结果被污染成「旧数据新」；`fillReset` 先 `b.Data[:0]` 再写、归还前再截零，结果恒为「新」。真正的修复不在 Get/Put，而在「取出先重置、归还前再重置」这条纪律，以及 `New` 必须能独立造出干净对象。

原子侧的正确性同样有边界。`atomic.Int64.Add`、`CompareAndSwap` 落到一条 CPU 原子读改写指令（x86 的 `LOCK CMPXCHG`、ARM 的 `LDXR/STXR` 循环），Go 内存模型规定所有 `sync/atomic` 操作按「顺序一致」执行，因此它们之间自带 happens-before，不会互相覆盖。但 CAS 只比较「当前值是否等于期望值」，察觉不到「被改成别的值又改回来」的 ABA 问题，跨步不变式仍要退回 Mutex。`atomic.Value` 的 `Store` 首次决定具体类型，之后存不同类型会 panic `inconsistently typed value`，存 nil 同样 panic，读方 `Load` 后必须判类型断言的 ok。生产定位：用 `go test -race ./...` 暴露对复用顺序的错误依赖；用 `go test -bench=. -benchmem` 看 `allocs/op` 判断 Pool 是否真有收益——本机实测 `BenchmarkNoPool` 是 1 allocs/op、1024 B/op，`BenchmarkWithPool` 降到 0 allocs/op。收益只在「构造成本高、可丢弃、无外部资源」的对象上成立，小对象直接分配往往更快。

```go
package main

import (
	"sync"
	"testing"
)

type Buf struct{ Data []byte }

var bufPool = sync.Pool{
	New: func() any { return &Buf{Data: make([]byte, 0, 1024)} },
}

var sink []byte

func BenchmarkNoPool(b *testing.B) {
	for i := 0; i < b.N; i++ {
		sink = make([]byte, 0, 1024)
	}
}

func BenchmarkWithPool(b *testing.B) {
	for i := 0; i < b.N; i++ {
		bb := bufPool.Get().(*Buf)
		bb.Data = bb.Data[:0]
		bufPool.Put(bb)
	}
}

// 错误写法：取出后不重置直接 append，残留数据混进结果；归还时也没清
func fillNoReset(b *Buf, s string) []byte {
	b.Data = append(b.Data, s...)
	return append([]byte(nil), b.Data...)
}

// 正确写法：取出先截零，用完后归还前再截零
func fillReset(b *Buf, s string) []byte {
	b.Data = append(b.Data[:0], s...)
	out := append([]byte(nil), b.Data...)
	b.Data = b.Data[:0] // 归还前重置
	return out
}

// 从池取用并归还的包装：真正的 bug 是「忘了重置」
func withPool(fn func(*Buf, string) []byte, s string) []byte {
	b := bufPool.Get().(*Buf)
	out := fn(b, s)
	bufPool.Put(b)
	return out
}

func TestPoolReset(t *testing.T) {
	// -race 下 sync/pool.go 的 Put 会随机丢弃对象，复用顺序不可依赖，
	// 因此直接构造「带残留」的脏对象，让实验稳定复现
	dirty := &Buf{Data: []byte("旧数据")}
	if got := string(fillNoReset(dirty, "新")); got != "旧数据新" {
		t.Fatalf("未重置应产生污染，实际 %q", got)
	}
	clean := &Buf{Data: []byte("旧数据")}
	if got := string(fillReset(clean, "新")); got != "新" {
		t.Fatalf("重置后应干净，实际 %q", got)
	}
	// 正确写法放进真实 Pool 也稳定
	if got := string(withPool(fillReset, "新")); got != "新" {
		t.Fatalf("Pool 中重置写法应稳定，实际 %q", got)
	}
}
```

```bash
go test -race -run=TestPoolReset -v ./...   # 打印污染结果，验证重置写法
go test -bench=. -benchmem -run=^$ ./...    # 看 allocs/op：NoPool 1、WithPool 0
go test -race ./...                         # 全量回归
```

<OutputBox title="本机实测（Go 1.23.8 / darwin-arm64）" output="BenchmarkNoPool-12：1 allocs/op，1024 B/op；BenchmarkWithPool-12：0 allocs/op，0 B/op" note="Pool 把每轮一次堆分配降到零；只有构造成本高、可丢弃的对象才值得这样复用" />

把分配、GC 行为与边界整理成表，判断「该不该用 Pool」时可逐行核对：

| 手段 / 状态 | 分配特征（实测） | GC 后的行为 | 适用与误用边界 |
| --- | --- | --- | --- |
| 每次 `make([]byte,0,1024)` | 1 allocs/op，1024 B/op | 对象即垃圾，触发 GC | 简单直接，小对象往往比 Pool 更快 |
| `sync.Pool` 复用 | 0 allocs/op，0 B/op | primary 降级为 victim，两轮后丢弃 | 只放可重建、无外部资源的临时大对象 |
| Pool 对象 `private` 槽 | 命中时零分配 | 每轮 GC 后被降级 | 不能假定 Put 后必 Get 回同一个 |
| `-race` 下的 `Put` | 1/4 概率丢弃对象 | 同上 | 专门暴露对复用顺序的错误依赖 |
| `atomic.Value.Store` | 无锁 | 不受 GC 清空影响 | 类型必须一致，禁止存 nil，读判 ok |

下面这张时序图把 `private`、`victim`、两轮 GC 与 `New` 兜底串起来，配合前面的两次 `runtime.GC()` 示例一起看：

<SequenceDiagram title="sync.Pool 的两轮 GC 与 victim 降级" caption="首轮 GC 把 local 降级为 victim，次轮丢弃 victim，池空后 Get 回退到 New" :actors='["应用","primary(local)","victim","GC","New"]' :steps='[{"from":"应用","to":"primary(local)","label":"Put 归还","note":"进本 P 的 private","kind":"call"},{"from":"primary(local)","to":"应用","label":"Get 命中","note":"LIFO 零分配复用","kind":"return"},{"from":"GC","to":"primary(local)","label":"首轮 GC","note":"STW 调 poolCleanup","kind":"call"},{"from":"primary(local)","to":"victim","label":"降级为 victim","note":"local 置空","kind":"call"},{"from":"应用","to":"victim","label":"Get 仍可能命中","note":"顶多活两轮 GC","kind":"call"},{"from":"GC","to":"victim","label":"次轮 GC","note":"清空 oldPools","kind":"call"},{"from":"victim","to":"New","label":"缓存丢弃","note":"对象被回收","kind":"call"},{"from":"应用","to":"New","label":"池空回退","note":"New 造干净对象","kind":"call"},{"from":"New","to":"应用","label":"返回新对象","note":"不依赖上次残留","kind":"return"}]' />

<Predict q="把带『旧数据』残留的 Buf 交给未重置的 fillNoReset 再 append『新』，结果是什么？" :options='["只有 新","旧数据新","空字符串","panic"]' answer="1" explain="复用对象带着上次残留，未截零就 append 会把旧内容拼在前面；正确写法先 b.Data[:0] 再写、归还前再截零，结果恒为 新。" />

<Callout type="tip" title="能不能用 -race 验证 Pool 复用？" text="不能验证『Put 必被复用』，因为 sync/pool.go 在 race.Enabled 时以 1/4 概率随机丢弃 Put 的对象，正是为了打掉这种错误假设。想稳定复现残留污染，要像 TestPoolReset 那样直接构造脏对象，或显式连调两次 runtime.GC() 观察 victim 被清空。" />

## 4. 工程实践
实战里 atomic 常用于指标计数、限流令牌、状态标志等单值场景，简单逻辑下比 Mutex 快一个量级；但「先检查再更新」这类跨步不变式要退回 Mutex 或 CAS 自旋。sync.Pool 只复用「构造成本高、可丢弃、无外部资源」的临时对象，是否有收益用基准测试的分配次数说话，小对象直接分配往往更快。

<Compare :panels='[{"label":"写法 A：互斥锁计数","code":"mu.Lock(); total = total + 1; mu.Unlock()","note":"通用，能护一段复合逻辑，高竞争要阻塞切换"},{"label":"写法 B：原子加","code":"total.Add(1)","note":"单条指令完成，只适合简单计数"}]' title="计数场景的两种写法" />

### 与 L10 实战的呼应

无锁与复用的收益边界，在极端流量下最容易被量化。第 116 讲 [秒杀与高并发库存](/projects/micro-seckill) 用原子预扣与一次性配额把十万级 QPS 削成数据库可承受的匀速写，「绝不超卖」靠的正是 `sync/atomic` 的顺序一致与 CAS 把「读改写」合成一步；分段库存把热点拆开，也沿用了「只护一个变量就别上锁」的取舍。第 110 讲 [分布式任务调度](/projects/concurrent-scheduler) 的 fencing token 是单调递增令牌、写入端只接受更大的值，思路与 CAS 的「比较后更新」同源，只是跨进程要靠 etcd 的线性一致而非单机原子指令。第 109 讲 [并发爬虫](/projects/concurrent-crawler) 复用 HTTP 缓冲区与解析缓冲时可用 `sync.Pool` 压分配，但必须记住 GC 会清空、取出先重置——把连接或带外部资源的对象放进 Pool，就会在下一轮 GC 后拿到脏对象；是否有收益，用本讲的 `go test -bench=. -benchmem` 看 `allocs/op` 说话。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l4-concurrency/lesson-046-atomic-pool`](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-046-atomic-pool)。clone 仓库后进入 `examples/l4-concurrency`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="用原子值统计命中次数" task="用 atomic.Int64 统计 100 次命中并打印总数，要求全程不加锁。" :hints='["声明 var hits atomic.Int64","用 hits.Add(1) 累加","用 hits.Load() 读结果"]' solution="var hits atomic.Int64\nfor i := 0; i != 100; i++ {\n    hits.Add(1)\n}\nfmt.Println(hits.Load())" explain="Add 不可分割，100 次累加不覆盖，Load 读到 100；换成普通整数并发累加会丢更新，正是第 049 讲的数据竞争。" />

## 随堂检测
<Quiz no="046-1" q="关于 sync.Pool，下面说法正确的是？" :options='["Put 的对象一定会在下次 Get 拿到","适合存放长期保留的数据库连接","GC 可能随时清空 Pool 中的对象","Pool 并发不安全需额外加锁"]' answer="2" explain="Pool 对象可能在 GC 时被移除，不能假定 Put 后必复用；它只缓存可重建的临时对象，本身并发安全。" />
<Predict no="046-2" q="8 个 goroutine 各对同一 atomic.Int64 执行 1000 次 Add(1)，最后 Load 的结果是？" :options='["小于 8000 的随机值","恰好 8000","取决于 GOMAXPROCS","运行时 panic"]' answer="1" output="8000" explain="原子加不可分割，任何更新都不被覆盖，结果与串行累加一致。" />

## 一页纸速记
<KnowledgeCard :points='["类型化 atomic 零值可用，Add、Load、CAS 护单变量","CAS 仅在值等于期望时更新，是无锁重试基石","atomic.Value 整体替换配置，读无锁","sync.Pool 用 Get、Put、New 复用对象","GC 会清空 Pool"]' :pitfalls='["把 Pool 当持久缓存或连接池","取出不重置就用，Put 后仍持引用"]' summary="atomic 用单指令换掉锁排队，sync.Pool 用复用换掉分配，但语义都弱于锁" />

## 小结与下一讲

- 原子操作适合单个数值或标志，快但不能替代锁守护跨步不变式；CAS 是无锁算法基础。
- atomic.Value 用整份替换实现无锁读热更新，Store 类型须一致且不能为 nil。
- sync.Pool 只是可被 GC 清空的周转区，New 须能独立造干净对象，归还前要重置。

[下一讲：context：取消、超时、值传递](/concurrency/context)
