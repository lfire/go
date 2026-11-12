---
title: 第 97 讲 · channel / mutex 源码与锁竞争
description: 沿 chansend/chanrecv/selectgo 与 Mutex 状态机，解释高并发下的性能拐点与定位手段
---

<StageHeader stage="L9 · 资深进阶与源码" level="★★★★★" time="55 分钟" goal="解释高并发下的性能拐点" prereq="第 43 讲 · channel 底层 hchan / 第 45 讲 · sync 原语" />

# 第 97 讲 · channel / mutex 源码与锁竞争

> 第 43 讲拆过 hchan 的结构，第 45 讲介绍过 sync 原语的用法。本讲把两者接回源码：看 chansend 与 chanrecv 怎样用两个等待队列直接配对，selectgo 怎样随机选就绪的 case，以及 Mutex 的 state 状态机怎样在普通模式、自旋与饥饿模式之间切换。理解了这些，才能解释「为什么并发一上来吞吐突然掉下去」，并知道该用哪种 profile 去定位。源码以 Go 1.23.8 为准。

## 这一讲你会得到什么
<CheckList id="L9-97" title="本讲目标" :items='["说出 hchan 的关键字段与 chansend 的快速路径", "解释 Mutex 状态机、自旋与 1ms 饥饿阈值", "用 mutex profile 与量化对比定位锁竞争拐点"]' />

## 1. 先建立直觉
把 channel 想成一个带两个等候区的中转台：一边是等货的接收者 recvq，一边是等交件的发送者 sendq。只要两边有人，值就直接从发送者手里递到接收者手里，根本不进仓库（缓冲区）；只有一边暂时没人，才把货放进环形货架或直接让 goroutine 睡下。Mutex 则是另一套：「先抢一下、抢不到转几圈、还抢不到排队，排太久就改成严格叫号」。前者靠配对减少拷贝，后者靠状态位在吞吐与公平之间权衡。
<ConceptDiagram type="hchan" title="hchan 的收发队列" caption="recvq 与 sendq 让收发双方直接配对，buf 只在没有等待者时才承接元素" />

## 2. 语法与规则
先定位签名与字段，再用程序验证行为。以下均来自 Go 1.23.8 的 GOROOT。

- `src/runtime/chan.go:33`：`type hchan struct`，含 `qcount`、`dataqsiz`、`buf`、`sendx`、`recvx`、`recvq`、`sendq`、`lock mutex`。
- `src/runtime/chan.go:171`：`func chansend(c *hchan, ep unsafe.Pointer, block bool, callerpc uintptr) bool`；`504` 行 `func chanrecv(...) (selected, received bool)`。
- `src/runtime/select.go:121`：`func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool)`。
- `src/sync/mutex.go:36`：`type Mutex struct { state int32; sema uint32 }`；`47` 行起定义 `mutexLocked`、`mutexWoken`、`mutexStarving` 位；`77` 行 `starvationThresholdNs = 1e6`，即 1ms。
- `src/runtime/lock_sema.go:29`：`active_spin = 4`、`passive_spin = 1`，自旋次数上限。

三段程序分别验证无缓冲配对、环形缓冲队列、以及锁竞争带来的耗时差异。

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

// 观察一：无缓冲 channel 的发送者会挂到 sendq，直到接收者出现。
func main() {
	ch := make(chan int)
	sent := make(chan struct{})
	go func() {
		ch <- 42 // 无接收者时阻塞，进入 hchan.sendq
		close(sent)
	}()

	time.Sleep(20 * time.Millisecond)
	fmt.Println("发送阻塞期间的 G 数 =", runtime.NumGoroutine())
	select {
	case <-sent:
		fmt.Println("不应发生：接收前就发送成功")
	default:
		fmt.Println("发送仍阻塞，符合预期")
	}
	fmt.Println("收到值 =", <-ch)
	<-sent
	fmt.Println("接收后 G 数 =", runtime.NumGoroutine())
}
```

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

// 观察二：带缓冲 channel 用环形队列 buf 承载元素，len(ch) 即 hchan.qcount。
func main() {
	ch := make(chan string, 3)
	ch <- "a"
	ch <- "b"
	ch <- "c"
	fmt.Println("已入队元素数 =", len(ch))
	fmt.Println("队列容量 =", cap(ch))

	blocked := make(chan struct{})
	go func() {
		ch <- "d" // 缓冲区满，挂到 sendq
		close(blocked)
	}()
	time.Sleep(20 * time.Millisecond)
	fmt.Println("满队列后阻塞的 G 数 =", runtime.NumGoroutine())
	fmt.Println("取出 =", <-ch)
	<-blocked
	fmt.Println("取出后队列长度 =", len(ch))
}
```

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// 观察三：高并发下锁竞争成为拐点，单一计数器用原子操作更划算。
func main() {
	const n = 100000
	var mu sync.Mutex
	var counter int64
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < n; j++ {
				mu.Lock()
				counter++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	lockCost := time.Since(start)

	var atomicCounter int64
	start = time.Now()
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < n; j++ {
				atomic.AddInt64(&atomicCounter, 1)
			}
		}()
	}
	wg.Wait()
	atomicCost := time.Since(start)

	fmt.Println("互斥锁结果 =", counter)
	fmt.Println("原子操作结果 =", atomic.LoadInt64(&atomicCounter))
	fmt.Println("互斥锁耗时 =", lockCost)
	fmt.Println("原子操作耗时 =", atomicCost)
}
```

<OutputBox title="第一段程序输出示例" output="发送阻塞期间的 G 数 = 2\n发送仍阻塞，符合预期\n收到值 = 42\n接收后 G 数 = 1" note="sendq 里挂着一个 sudog 时 NumGoroutine 为 2；一旦接收者取走值，发送者被唤醒并直接完成拷贝。" />

<LineByLine title="chansend 的快速路径" code="if c.recvq.first != nil {\n    send(c, sg, ep, unlock, 3)\n    return true\n}\nenqueue(c, ep) // buf 未满则入环形队列" :notes='["先看 recvq 有没有等待的接收者，有就直接把值交给它，不进缓冲区", "send 完成 sudog 与值的直接交接，这是最省拷贝的一条路径", "交接成功立即返回，整个过程持有 c.lock 保证互斥", "没有接收者且缓冲未满时把元素放进 buf，并更新 sendx 与 qcount"]' />

## 3. 深入一步
chansend 的第一段是「无锁快速失败」：非阻塞发送若发现通道已满且未关闭，直接返回而不加锁。加锁之后才依次处理关闭 panic、recvq 直接交接、缓冲区入队，最后才把自己包装成 sudog 挂到 sendq 并 gopark。chanrecv 与之对称。selectgo 则先按 pollorder 随机打乱 case 检查一遍是否就绪，全不就绪时把所有 case 挂到对应等待队列，被唤醒后再按 lockorder 按地址顺序加锁解锁，避免死锁。

Mutex 的 `state` 是一个 int32 位图：低位是 `mutexLocked`，另有 `mutexWoken` 与 `mutexStarving`，高位存等待者数量。抢锁先 CAS，失败进 `lockSlow`：满足 `active_spin` 条件时先自旋几轮，之后挂到 sema 休眠；若某个等待者超过 1ms 没拿到锁，就置 `mutexStarving` 切到饥饿模式，此后锁直接交给队首、新来者不许自旋，直到队列清空或等待时间回落才切回普通模式。

<Term word="饥饿模式" text="Mutex 在等待者超过 starvationThresholdNs（1ms）后进入的模式：所有权直接交给队首，新 goroutine 不抢不自旋，用吞吐换尾延迟的公平性。" />

| 字段或常量 | 位置（Go 1.23.8） | 含义 |
| --- | --- | --- |
| `recvq` / `sendq` | `chan.go` 的 `hchan` | 阻塞接收者与发送者的 sudog 双向队列 |
| `buf` / `sendx` / `recvx` | `chan.go` 的 `hchan` | 环形缓冲区及读写下标 |
| `qcount` / `dataqsiz` | `chan.go` 的 `hchan` | 当前元素数与队列容量 |
| `state` / `sema` | `sync/mutex.go:36` | 状态位图与休眠信号量 |
| `mutexStarving` / `mutexWoken` | `sync/mutex.go:47` | 饥饿与唤醒标志位 |
| `starvationThresholdNs` | `sync/mutex.go:77` | 1ms，超过即切饥饿模式 |
| `active_spin = 4` | `runtime/lock_sema.go:29` | 进入休眠前的自旋次数上限 |

<Callout type="warn" title="先减临界区，再谈换锁" text="看到锁竞争别急着换无锁结构：先缩短临界区、把 I/O 移出锁外，再考虑分片或 atomic。自旋只在多核且临界区极短时划算。" />

## 4. 工程实践
定位锁竞争有两把尺子：`GODEBUG=mutexprofilefraction=1` 打开 mutex profile，配合 `go tool pprof` 看 `contentions` 与 `delay`；`go tool trace` 的 Synchronization 视图则能看到 goroutine 因阻塞在锁上的时间线。判断拐点的办法是固定 worker 数逐级加压，记录每级 QPS 与 p99，一旦 QPS 不增而 p99 陡升，多半就是锁或 channel 的配对点成了瓶颈。

```bash
GODEBUG=mutexprofilefraction=1 ./myserver
go tool pprof -top mutex.prof
```

<Compare title="共享状态的两种保护方式" :panels='[{"label":"写法 A：一把大锁","code":"mu.Lock()\ncache[k] = v\nmu.Unlock()","note":"临界区含 map 写入，并发越高等待者越多，退化到饥饿模式后吞吐骤降"},{"label":"写法 B：按 key 分片","code":"shard := shardFor(k)\nshard.mu.Lock()\nshard.data[k] = v\nshard.mu.Unlock()","note":"把一把锁拆成 N 把，竞争面缩小到同分片的 key，配合 profile 验证收益"}]' />

## 动手练一练
<Exercise title="用 mutex profile 找到最热的锁" task="给第三段程序开启 mutexprofilefraction，用 pprof 看哪一行 Lock 的 contentions 最高，再改成 atomic 版本对比。" :hints='["采样 rate 设为 1 才能抓到全部竞争", "关注 contentions 与 delay 两列"]' solution="GODEBUG=mutexprofilefraction=1\nmu.Lock()\ncounter = counter + 1\nmu.Unlock()\natomic.AddInt64(ptr, 1)" explain="竞争会集中到临界区那一行；改成 atomic 后热点消失。完整代码见第三段。" />

## 随堂检测
<Quiz no="97-1" q="chansend 在有等待接收者时会怎么做？" :options='["把值拷进 buf 环形队列", "直接把值交给 recvq 队首的 sudog", "先复制到堆再唤醒接收者", "返回 false 让调用方重试"]' answer="1" explain="chansend 加锁后先查 recvq，非空则直接交接，值不进缓冲区。" />
<Predict no="97-2" q="一个等待者超过 1ms 仍没拿到 Mutex，会发生什么？" :options='["继续自旋直到成功", "切换为饥饿模式，锁直接交给队首", "直接 panic", "把 GOMAXPROCS 加一"]' answer="1" output="切换为饥饿模式，锁直接交给队首" explain="starvationThresholdNs 为 1ms；超时后置 mutexStarving，所有权由 Unlock 直接移交队首，新来者不再自旋。" />

## 一页纸速记
<KnowledgeCard :points='["hchan 用 recvq/sendq 直接配对，buf 只兜底", "selectgo 随机 pollorder 查就绪，lockorder 按地址加锁", "Mutex 超过 1ms 等待切饥饿模式"]' :pitfalls='["临界区里做 I/O：把竞争放大", "单核场景指望自旋：会被直接跳过", "不看 profile 就换无锁结构"]' summary="性能拐点往往不在算法，而在共享状态的配对与排队方式上。" />

## 小结与下一讲
1. chansend/chanrecv 先尝试无锁快速路径，再在 c.lock 下做直接交接、入队或 gopark。
2. Mutex 的 state 位图配合自旋与 1ms 饥饿阈值，在吞吐与公平之间动态切换。
3. 用 mutex profile 与逐级加压定位拐点，先缩短临界区，再考虑分片或原子操作。

[下一讲：编译器源码：SSA、逃逸与内联](/advanced/compiler-ssa)
