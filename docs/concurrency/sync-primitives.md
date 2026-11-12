---
title: 第 45 讲 · sync：Mutex / RWMutex / WaitGroup / Once / Cond
description: 在共享内存场景选对原语，掌握互斥锁、读写锁、WaitGroup、Once、Cond 的语义、粒度与常见死锁
---

<StageHeader stage="L4 · 并发编程" level="★★★☆☆" time="45 分钟" goal="在共享内存场景选对原语" prereq="第 44 讲 · select、超时与多路复用" />

# 第 45 讲 · sync：Mutex / RWMutex / WaitGroup / Once / Cond

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

## 4. 工程实践
锁的首选是「不用锁」：能用 channel 传所有权、用不可变数据就别引入共享状态。确实要共享时按读写比例选锁——写多或临界区极短时普通 Mutex 更快，RWMutex 读锁记账有成本。锁和数据定义在一起，导出方法内部加解锁；初始化用 Once，广播退出优先 close(channel)，比 Cond 简洁。

<Compare :panels='[{"label":"写法 A：Add 放进协程内","code":"go func() { wg.Add(1); defer wg.Done(); work() }(); wg.Wait()","note":"主协程可能先看到计数为 0 就返回，任务未统计"},{"label":"写法 B：Add 放在启动前","code":"wg.Add(1); go func() { defer wg.Done(); work() }(); wg.Wait()","note":"计数在 Wait 之前登记，语义确定，唯一推荐写法"}]' title="WaitGroup.Add 的位置" />

## 动手练一练
<Exercise title="用 WaitGroup 等待一批任务" task="启动 5 个 goroutine 各打印编号，主协程等全部结束再退出，要求正确放置 Add、Done、Wait。" :hints='["Add 在 go 之前调用","函数体内 defer wg.Done()","循环结束再 wg.Wait()"]' solution="var wg sync.WaitGroup\nfor i := 0; i != 5; i++ {\n    wg.Add(1)\n    go func(id int) {\n        defer wg.Done()\n        fmt.Println(id)\n    }(i)\n}\nwg.Wait()" explain="先 Add 登记再启动，defer Done 保证计数，循环外 Wait 阻塞到归零。把 Add 挪进 goroutine 是最常见错误，会让主协程提前退出。" />

## 随堂检测
<Quiz no="45-1" q="关于 RWMutex，下面说法正确的是？" :options='["多个写锁可同时持有","读锁互斥，读多时很差","读多写少时多个 RLock 可并发，写锁独占","同一协程可递归加两次 RLock"]' answer="2" explain="RWMutex 允许多读并存、写锁独占；读写都频繁时普通 Mutex 更快，且 Go 的锁都不可重入。" />
<Predict no="45-2" q="5 个 goroutine 并发调用 sync.Once 保护的 loadConfig，『真正执行昂贵的初始化』打印几次？" :options='["1 次","5 次","0 次","取决于调度"]' answer="0" output="真正执行昂贵的初始化（恰好 1 次）" explain="sync.Once 用互斥与原子标志保证 Do 的函数体全局只执行一次，所有调用者都等首次初始化完成才返回。" />

## 一页纸速记
<KnowledgeCard :points='["Mutex 守临界区，RWMutex 适合读多写少","Add 必须在 go 之前，Done 配 defer，Wait 放循环外","sync.Once 保证初始化只跑一次","Cond 绑定锁，Wait 用 for 复查，Broadcast 唤醒全部","广播退出优先 close(channel)"]' :pitfalls='["Add 放进 goroutine 导致 Wait 提前返回","复制已用 Mutex、忘 Unlock、递归加锁","两把锁相反顺序造成 AB-BA 死锁"]' summary="按需求选原语：保护共享数据用锁，等一组任务用 WaitGroup，一次性初始化用 Once，等条件用 Cond" />

## 小结与下一讲

- Mutex/RWMutex 保护共享状态，临界区小而完整，解锁交给 defer；不可重入、不可复制。
- Add 必须在启动 goroutine 之前，Once 让初始化只发生一次，Cond 等待要用 for 复查条件。
- 固定加锁顺序、优先用 channel 传所有权，是少写死锁的关键。

[下一讲：atomic 与 sync.Pool](/concurrency/atomic-pool)
