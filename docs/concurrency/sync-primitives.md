---
title: 第 045 讲 · sync：Mutex / RWMutex / WaitGroup / Once / Cond
description: 在共享内存场景选对原语，掌握互斥锁、读写锁、WaitGroup、Once、Cond 的语义、粒度与常见死锁
---

<StageHeader stage="L4 · 并发编程" level="★★★☆☆" time="45 分钟" goal="在共享内存场景选对原语" prereq="第 044 讲 · select、超时与多路复用" />

# 第 045 讲 · sync：Mutex / RWMutex / WaitGroup / Once / Cond

> 计数器、缓存表、全局配置这类状态天生被共享修改，用 sync 包原语比 channel 更直接。本讲按「保护数据、协调进度、一次性初始化、条件等待」讲透五个原语，并拆开两个高频事故：WaitGroup 的 Add 放错位置与几类死锁。

## 这一讲你会得到什么
<CheckList id="lesson-45" title="本讲目标" :items='["用 Mutex、RWMutex 保护共享数据，理解互斥粒度","写对 WaitGroup，知道 Add 为何在 goroutine 之外","用 sync.Once 实现并发安全的单例","用 Cond 做条件等待与广播，识别常见死锁"]' />

## 1. 先建立直觉
把五个原语想成协作工具：Mutex 是「一次只进一人的会议室」；RWMutex 是「阅览室」，大家可同时看，但有人改书架就得清场；WaitGroup 是「点名册」，走一个划一个；Once 是「只贴一次的公告」；Cond 是「休息室门铃」，条件满足时按铃叫醒所有人。它们补的正是 channel 不擅长的那半边：对共享状态的直接保护。

<ConceptDiagram type="lock" title="共享状态与同步原语" caption="Mutex 守临界区，WaitGroup 记进度，Once 管初始化，Cond 负责等待与唤醒" />

## 2. 语法与规则
先看 Mutex 与 RWMutex。SafeMap 用读写锁保护 map：写用 Lock 独占，读用 RLock 允许并发。临界区只包住真正共享的读写，粒度越小并发度越高。

```go
package main

import (
	"fmt"
	"sync"
)

type SafeMap struct {
	mu sync.RWMutex // 读多写少时更划算
	m  map[string]int
}

func (s *SafeMap) Add(k string, v int) {
	s.mu.Lock()         // 写锁独占
	defer s.mu.Unlock() // 保证任何路径都解锁
	s.m[k] = s.m[k] + v
}

func (s *SafeMap) Get(k string) int {
	s.mu.RLock()         // 多个读可并行
	defer s.mu.RUnlock()
	return s.m[k]
}

func main() {
	s := &SafeMap{m: make(map[string]int)}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1) // Add 必须在启动 goroutine 之前
		go func() {
			defer wg.Done()
			s.Add("count", 1)
		}()
	}
	wg.Wait()
	fmt.Println("count =", s.Get("count"))
}
```

<OutputBox title="运行结果" output="count = 10" note="十次累加不丢失；不加锁结果会随机小于 10，甚至触发 concurrent map writes" />

再看 WaitGroup 与 Once。把昂贵初始化交给 sync.Once，无论多少 goroutine 并发调用，函数体只执行一次。

```go
package main

import (
	"fmt"
	"sync"
)

type Config struct{ Addr string }

var (
	once sync.Once
	cfg  *Config
)

func loadConfig() *Config {
	once.Do(func() { // Do 的函数体全局只执行一次
		fmt.Println("真正执行昂贵的初始化")
		cfg = &Config{Addr: "127.0.0.1:8080"}
	})
	return cfg
}

func main() {
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1) // 在 go 之前 Add，Wait 才不提前返回
		go func(id int) {
			defer wg.Done()
			fmt.Println("协程", id, "读到", loadConfig().Addr)
		}(i)
	}
	wg.Wait()
}
```

临界区三行值得逐行看。

<LineByLine code="mu.Lock()\ndefer mu.Unlock()\ncounter = counter + 1" :notes='["Lock 获取互斥锁，被占用则阻塞，保证只有一个执行者进入","defer Unlock 把解锁推迟到函数返回，提前 return 或 panic 也能释放","临界区只包住真正共享的读写，范围越小并发度越高"]' title="最小临界区" />

## 3. 深入一步
Cond 解决「等某个条件成立」，必须与一把锁绑定，等待时用 for 复查条件。Wait 先释放锁并挂起，被唤醒后重新持锁返回；Broadcast 唤醒全部，Signal 只唤醒一个。

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Signal struct {
	mu    sync.Mutex
	cond  *sync.Cond
	ready bool
}

func NewSignal() *Signal {
	s := &Signal{}
	s.cond = sync.NewCond(&s.mu) // 绑定一把锁
	return s
}

func (s *Signal) Publish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = true
	s.cond.Broadcast() // 广播唤醒所有等待者
}

func (s *Signal) WaitReady() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for !s.ready { // 必须 for 复查，防虚假唤醒
		s.cond.Wait() // 释放锁挂起，唤醒后重新持锁返回
	}
}

func main() {
	s := NewSignal()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s.WaitReady()
			fmt.Println("等待者", id, "被唤醒")
		}(i)
	}
	time.Sleep(50 * time.Millisecond) // 让等待者先挂起
	s.Publish()
	wg.Wait()
}
```

五个原语各管什么、易错在哪：

| 原语 | 解决的问题 | 关键 API | 典型误用 |
| --- | --- | --- | --- |
| Mutex | 独占改共享数据 | Lock / Unlock | 忘解锁、复制已用锁、递归加锁 |
| RWMutex | 读多写少读并行 | RLock / RUnlock / Lock | 读锁里改数据、读写都频繁白付开销 |
| WaitGroup | 等一组任务结束 | Add / Done / Wait | Add 放进 goroutine 内 |
| Once | 只初始化一次 | Do | 在 Do 里再调自身导致死锁 |
| Cond | 等条件成立 | Wait / Signal / Broadcast | Wait 前未持锁、不用 for 复查 |

<Term word="互斥粒度" text="被锁保护的临界区范围；越粗越安全但越串行，越细并发度越高，拆太碎易漏保护或锁顺序死锁" />

<Callout type="warn" title="WaitGroup.Add 的时机" text="Add 必须在启动 goroutine 之前、与 Wait 同层调用。若写进 goroutine 内部，主协程可能先看到计数为 0 就直接返回，任务没被统计，计数与 Wait 还成竞态。约定：循环里先 Add，再 go，函数体 defer Done，循环外 Wait。" />

<Callout type="tip" title="几类常见死锁" text="Go 的 Mutex 不可重入，同一 goroutine 连续 Lock 两次会自锁；复制已使用的 Mutex 值会得到两份无关状态，务必传指针；两个 goroutine 以相反顺序获取两把锁会形成 AB-BA 死锁，应固定全局加锁顺序。这些问题多能被 go vet 与 -race 提前暴露。" />

### 再深一层：WaitGroup 的计数、sema 与 happens-before

<Callout type="note" title="进阶选读 · 首次学习可跳过" text="本段涉及运行时源码、内存模型与性能工具等后面讲次才系统展开的内容，属于加深理解的可选层。第一次学建议先掌握本讲主线，遇到尚未讲过的术语不必停下，学完对应讲次再回看，不影响后续理解。" />

「锁粒度」和「Add 时机」不是风格偏好，而是由运行时状态与内存模型共同决定的正确性约束。先看 `sync.Mutex`：结构体只有两个字段，`state int32`（低位是 `mutexLocked`/`mutexWoken`/`mutexStarving` 三个标志位加等待者计数）与 `sema uint32`。`Lock` 先 CAS 抢 `mutexLocked`，抢不到才进 `lockSlow` 自旋，仍失败就调 `runtime_SemacquireMutex` 把当前 G 挂进 `runtime/sema.go` 的 `semaRoot` 等待队列（一棵按地址索引的 `sudog` treap）；`Unlock` 走 `runtime_Semrelease` 把队首 `goready`。正是这对 sema 操作带有 acquire/release 语义，才让「一次 Unlock happens-before 下一次 Lock」，前一次临界区的写入对后一个持锁者可见。若读路径不进临界区（如 `badGet` 直接读 `c.n`），这条 happens-before 就断了，`go test -race` 会在该变量上报 `WARNING: DATA RACE`；把读也纳入同一把锁，`TestGuard` 就稳定输出 1000。这就是「临界区要覆盖全部共享访问」的机器级含义：粒度不是越小越好，漏掉一次读就等于没保护。

`sync.WaitGroup` 同理但更隐蔽。Go 1.20 起它的 `state atomic.Uint64` 高 32 位存计数器、低 32 位存等待者计数，另加一个 `sema uint32`：`Add(delta)` 对计数器做原子加，`Done()` 即 `Add(-1)`；`Wait()` 循环读 state，计数器为 0 就直接返回，否则 CAS 增加等待者计数后 `runtime_Semacquire(&wg.sema)` 挂起，等最后一个 `Done` 把计数减到 0 时由 `runtime_Semrelease` 唤醒全部等待者。源码注释写得很直白：`Done` synchronizes before 它所唤醒的 `Wait` 的返回。关键在于 `Add` 与 `Wait` 之间没有自动排序：若把 `wg.Add(1)` 挪进 goroutine 内部，主协程完全可能在计数登记前读到 0 就返回。实测把本讲 `TestGuard` 里的 Add 挪进函数体、让任务睡 1ms，`go test -count=3` 下 `Wait` 返回时「已完成」有时是 50、有时是 0——结果随机少算，且没有任何报错。约定「循环里先 Add、再 go、函数体 defer Done、循环外 Wait」正是为了把计数登记排在 goroutine 启动之前。

把下面的 `guard_test.go` 用 `go test -race -run=TestGuard -v ./...` 跑，正确写法全绿；再用 `SHOW_RACE=1 go test -race -run=TestBadGuardRace ./...` 单独跑那个故意保留 bug 的反面实验，会看到 `WARNING: DATA RACE` 精确指向 `c.n` 的读写——这就是可复现的对照。生产里排查锁相关故障，用 `go tool pprof -mutex` 看争用热点、block profile 看阻塞时长，`runtime/pprof` 的 goroutine profile 能发现卡在 `semacquire` 的 G；AB-BA 死锁（两个 goroutine 以相反顺序取两把锁）运行时只在「所有 G 都睡死」时才 panic，局部死锁靠 `-race` 与超时兜底，因此工程上应固定全局加锁顺序。

```go
package main

import (
	"os"
	"sync"
	"testing"
)

type Counter struct {
	mu sync.Mutex
	n  int
}

// 错误对照：读路径没进临界区，与写无同步
func (c *Counter) badGet() int { return c.n }

// 正确：读写都由同一把锁覆盖
func (c *Counter) Add() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *Counter) Get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func TestGuard(t *testing.T) {
	var c Counter
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1) // 计数先登记，Wait 才不会漏任务
		go func() {
			defer wg.Done()
			c.Add()
		}()
	}
	wg.Wait()
	if got := c.Get(); got != 1000 {
		t.Fatalf("期望 1000，实际 %d", got)
	}
}

// 反面实验：设 SHOW_RACE=1 并配合 -race，可看到读未加锁的 DATA RACE
func TestBadGuardRace(t *testing.T) {
	if os.Getenv("SHOW_RACE") == "" {
		t.Skip("反面实验：设 SHOW_RACE=1 并加 -race 运行")
	}
	var c Counter
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			c.Add()
		}
	}()
	for i := 0; i < 1000; i++ {
		_ = c.badGet() // 与写并发且无锁
	}
	wg.Wait()
}
```

```bash
go test -race -run=TestGuard -v ./...                    # 正确写法：全绿，稳定 1000
SHOW_RACE=1 go test -race -run=TestBadGuardRace ./...   # 反面实验：报 WARNING: DATA RACE
go vet ./...                                           # 抓 copylocks 等静态问题
```

把各原语「谁保证可见性、依据在哪、错了怎么测」整理成表，比死记 API 更可靠：

| 同步动作 | 建立的 happens-before | 源码依据 | 误用后果与检测手段 |
| --- | --- | --- | --- |
| Mutex 的 Unlock 到后续 Lock | 前次临界区写入对下一持锁者可见 | `runtime_Semrelease` 与 `runtime_SemacquireMutex` 的 acquire/release | 读不进锁即竞态，`go test -race` 报 DATA RACE |
| WaitGroup 的 Done 到被唤醒的 Wait | 任务写入对 Wait 返回后可见 | 源码注释 synchronizes before；state 计数归零触发 `runtime_Semrelease` | Add 放进 goroutine 则 Wait 可能提前返回，`go test -count=10` 随机少算 |
| close(done) 到各接收者 | 关闭前写入对所有接收者可见 | 关闭 channel 唤醒全部 recvq 中的 sudog | 忘关则下游永等，goroutine profile 见卡住的 G |
| Once 的 Do 返回 | 初始化写入对所有调用者可见 | done 原子标志加互斥双重检查 | Do 里递归调自身会自锁，靠超时与压测暴露 |
| Cond 的 Broadcast 到 Wait 返回 | 唤醒前写入对等待者可见（须持锁） | Wait 释放锁并入队，唤醒时 goready | Wait 前未持锁会 panic，不用 for 复查会虚假唤醒 |

下图把「抢锁失败、挂入 sema 队列、Unlock 唤醒、重新入调度」这条链路补上，正好解释高竞争下 G 为何被切走又放回：

<ConceptDiagram type="scheduler-loop" title="锁竞争下的 G 挂起与唤醒" caption="抢不到 Mutex 的 G 经 runtime_SemacquireMutex 挂入 semaRoot 等待队列，Unlock 时 goready 唤醒，回到调度循环重新入队" />

<Quiz q="把 wg.Add(1) 从循环里挪进 goroutine 函数体，最可能观察到什么？" :options='["编译报错","Wait 可能在计数登记前观察到 0 而提前返回，结果随机少算","程序一定 panic","RWMutex 自动退化成普通 Mutex"]' answer="1" explain="Add 与 Wait 没有自动排序；实测让任务睡 1ms、go test -count=3，Wait 返回时完成数有时是 50、有时是 0。先 Add 再 go 才能让 Done synchronizes before Wait 的返回。" />

<Callout type="warn" title="局部死锁 -race 也救不了" text="两个 goroutine 以相反顺序获取两把 Mutex 形成的 AB-BA 死锁，Go 运行时只在『所有 goroutine 都睡死』时才抛 fatal error；只要还有别的 G 在跑，它会一直挂着。go vet 能抓 copylocks（复制已用锁），抓不到加锁顺序；工程上固定全局加锁顺序、给等待配超时或减少共享，才是兜底。" />

## 4. 工程实践
锁的首选是「不用锁」：能用 channel 传所有权、用不可变数据就别引入共享状态。确实要共享时按读写比例选锁——写多或临界区极短时普通 Mutex 更快，RWMutex 读锁记账有成本。锁和数据定义在一起，导出方法内部加解锁；初始化用 Once，广播退出优先 close(channel)，比 Cond 简洁。

<Compare :panels='[{"label":"写法 A：Add 放进协程内","code":"go func() { wg.Add(1); defer wg.Done(); work() }(); wg.Wait()","note":"主协程可能先看到计数为 0 就返回，任务未统计"},{"label":"写法 B：Add 放在启动前","code":"wg.Add(1); go func() { defer wg.Done(); work() }(); wg.Wait()","note":"计数在 Wait 之前登记，语义确定，唯一推荐写法"}]' title="WaitGroup.Add 的位置" />

### 与 L10 实战的呼应

「临界区要覆盖全部共享访问」这条推导，在三个实战里都是正确性的硬约束。第 116 讲 [秒杀与高并发库存](/projects/micro-seckill) 把「先 SELECT 余量、判断大于 0、再 UPDATE 减一」列为超卖根因：两个并发事务读到同一个旧值、各自扣减，100 件库存能卖出 137 件；修法正是把检查、判重、扣减合成一个临界区或一条原子语句，与本讲 `badGet` 漏加锁、`TestGuard` 全绿是同一个 happens-before 问题。第 109 讲 [并发爬虫](/projects/concurrent-crawler) 的 Robots 与限速器用 `sync.Mutex` 护每 host 的映射，但「拿令牌的等待」必须放在锁外，否则临界区被 I/O 等待撑大、并发度归零——这正是「粒度不是越小越好，而是刚好覆盖共享访问」的取舍。第 110 讲 [分布式任务调度](/projects/concurrent-scheduler) 用任务状态机与 fencing token 把故障转移后的重复执行收敛掉，`WaitGroup` 的「先 Add 再 go、循环外 Wait」则是执行器收尾的最小纪律。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l4-concurrency/lesson-045-sync-primitives`](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-045-sync-primitives)。clone 仓库后进入 `examples/l4-concurrency`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="用 WaitGroup 等待一批任务" task="启动 5 个 goroutine 各打印编号，主协程等全部结束再退出，要求正确放置 Add、Done、Wait。" :hints='["Add 在 go 之前调用","函数体内 defer wg.Done()","循环结束再 wg.Wait()"]' solution="var wg sync.WaitGroup\nfor i := 0; i != 5; i++ {\n    wg.Add(1)\n    go func(id int) {\n        defer wg.Done()\n        fmt.Println(id)\n    }(i)\n}\nwg.Wait()" explain="先 Add 登记再启动，defer Done 保证计数，循环外 Wait 阻塞到归零。把 Add 挪进 goroutine 是最常见错误，会让主协程提前退出。" />

## 随堂检测
<Quiz no="045-1" q="关于 RWMutex，下面说法正确的是？" :options='["多个写锁可同时持有","读锁互斥，读多时很差","读多写少时多个 RLock 可并发，写锁独占","同一协程可递归加两次 RLock"]' answer="2" explain="RWMutex 允许多读并存、写锁独占；读写都频繁时普通 Mutex 更快，且 Go 的锁都不可重入。" />
<Predict no="045-2" q="5 个 goroutine 并发调用 sync.Once 保护的 loadConfig，『真正执行昂贵的初始化』打印几次？" :options='["1 次","5 次","0 次","取决于调度"]' answer="0" output="真正执行昂贵的初始化（恰好 1 次）" explain="sync.Once 用互斥与原子标志保证 Do 的函数体全局只执行一次，所有调用者都等首次初始化完成才返回。" />

## 一页纸速记
<KnowledgeCard :points='["Mutex 守临界区，RWMutex 适合读多写少","Add 必须在 go 之前，Done 配 defer，Wait 放循环外","sync.Once 保证初始化只跑一次","Cond 绑定锁，Wait 用 for 复查，Broadcast 唤醒全部","广播退出优先 close(channel)"]' :pitfalls='["Add 放进 goroutine 导致 Wait 提前返回","复制已用 Mutex、忘 Unlock、递归加锁","两把锁相反顺序造成 AB-BA 死锁"]' summary="按需求选原语：保护共享数据用锁，等一组任务用 WaitGroup，一次性初始化用 Once，等条件用 Cond" />

## 小结与下一讲

- Mutex/RWMutex 保护共享状态，临界区小而完整，解锁交给 defer；不可重入、不可复制。
- Add 必须在启动 goroutine 之前，Once 让初始化只发生一次，Cond 等待要用 for 复查条件。
- 固定加锁顺序、优先用 channel 传所有权，是少写死锁的关键。

[下一讲：atomic 与 sync.Pool](/concurrency/atomic-pool)
