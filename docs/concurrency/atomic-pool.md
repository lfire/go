---
title: 第 46 讲 · atomic 与 sync.Pool
description: 理解原子操作、CAS、atomic.Value 与 sync.Pool 的对象复用，看清无锁与减少分配的收益、代价和 GC 清空语义
---

<StageHeader stage="L4 · 并发编程" level="★★★★☆" time="40 分钟" goal="理解无锁与对象复用的收益和代价" prereq="第 45 讲 · sync 原语" />

# 第 46 讲 · atomic 与 sync.Pool

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

## 4. 工程实践
实战里 atomic 常用于指标计数、限流令牌、状态标志等单值场景，简单逻辑下比 Mutex 快一个量级；但「先检查再更新」这类跨步不变式要退回 Mutex 或 CAS 自旋。sync.Pool 只复用「构造成本高、可丢弃、无外部资源」的临时对象，是否有收益用基准测试的分配次数说话，小对象直接分配往往更快。

<Compare :panels='[{"label":"写法 A：互斥锁计数","code":"mu.Lock(); total = total + 1; mu.Unlock()","note":"通用，能护一段复合逻辑，高竞争要阻塞切换"},{"label":"写法 B：原子加","code":"total.Add(1)","note":"单条指令完成，只适合简单计数"}]' title="计数场景的两种写法" />

## 动手练一练
<Exercise title="用原子值统计命中次数" task="用 atomic.Int64 统计 100 次命中并打印总数，要求全程不加锁。" :hints='["声明 var hits atomic.Int64","用 hits.Add(1) 累加","用 hits.Load() 读结果"]' solution="var hits atomic.Int64\nfor i := 0; i != 100; i++ {\n    hits.Add(1)\n}\nfmt.Println(hits.Load())" explain="Add 不可分割，100 次累加不覆盖，Load 读到 100；换成普通整数并发累加会丢更新，正是第 49 讲的数据竞争。" />

## 随堂检测
<Quiz no="46-1" q="关于 sync.Pool，下面说法正确的是？" :options='["Put 的对象一定会在下次 Get 拿到","适合存放长期保留的数据库连接","GC 可能随时清空 Pool 中的对象","Pool 并发不安全需额外加锁"]' answer="2" explain="Pool 对象可能在 GC 时被移除，不能假定 Put 后必复用；它只缓存可重建的临时对象，本身并发安全。" />
<Predict no="46-2" q="8 个 goroutine 各对同一 atomic.Int64 执行 1000 次 Add(1)，最后 Load 的结果是？" :options='["小于 8000 的随机值","恰好 8000","取决于 GOMAXPROCS","运行时 panic"]' answer="1" output="8000" explain="原子加不可分割，任何更新都不被覆盖，结果与串行累加一致。" />

## 一页纸速记
<KnowledgeCard :points='["类型化 atomic 零值可用，Add、Load、CAS 护单变量","CAS 仅在值等于期望时更新，是无锁重试基石","atomic.Value 整体替换配置，读无锁","sync.Pool 用 Get、Put、New 复用对象","GC 会清空 Pool"]' :pitfalls='["把 Pool 当持久缓存或连接池","取出不重置就用，Put 后仍持引用"]' summary="atomic 用单指令换掉锁排队，sync.Pool 用复用换掉分配，但语义都弱于锁" />

## 小结与下一讲

- 原子操作适合单个数值或标志，快但不能替代锁守护跨步不变式；CAS 是无锁算法基础。
- atomic.Value 用整份替换实现无锁读热更新，Store 类型须一致且不能为 nil。
- sync.Pool 只是可被 GC 清空的周转区，New 须能独立造干净对象，归还前要重置。

[下一讲：context：取消、超时、值传递](/concurrency/context)
