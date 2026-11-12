---
title: 第 109 讲 · 实战 07 · 并发爬虫：单机 → 并发 → 分布式
description: 同一爬虫业务三次架构演进，对比单线程、worker pool 与队列化分布式版在吞吐、去重、限速与失败域上的取舍
---

<StageHeader stage="L10 · 工程实战" level="★★★★☆" time="70 分钟" goal="同一业务三次架构演进" prereq="第 108 讲 · 实战 06 · CSV / SQL 查询工具" />

# 第 109 讲 · 实战 07 · 并发爬虫：单机 → 并发 → 分布式

> 架构是被瓶颈逼出来的。抓一百页和抓一千万页需要三套不同系统。本讲让同一业务连跑三次，重点是每次演进上一版在哪撑不住、新版本多付了什么代价。

## 这一讲你会得到什么
<CheckList id="l109-goals" title="本讲目标" :items='["说清单线程把时间浪费在哪里，并发为何提速","用 seen、WaitGroup、限速 ticker 写对 worker pool","理解进程内状态为何是分布式演进的根本障碍","用租约、重投与幂等键处理 worker 崩溃"]' />

## 1. 项目目标与约束

| 维度 | 目标 | 约束 |
| --- | --- | --- |
| 业务 | 从种子页抓页面、抽链接、继续抓 | 三版保持相同语义 |
| 正确性 | 同一 URL 只抓一次 | 去重覆盖所有在途任务 |
| 礼貌 | 控制请求速率 | 限速全局，不是每 worker 一份 |
| 演进 | 每版只解决一个瓶颈 | 不提前引入下一版复杂度 |

## 2. 架构蓝图与演进

<FlowTimeline :steps='[{"title":"V1 单线程","desc":"一个队列一个循环串行完成","tone":"blue"},{"title":"V2 并发","desc":"派给 N 个 worker，共享 seen 与全局 ticker","tone":"green"},{"title":"V3 分布式","desc":"队列、去重、限速外置，可横向扩","tone":"red"}]' direction="vertical" />

<ConceptDiagram type="mq-flow" title="V3 队列化分布式版" caption="调度器只往队列投放，无状态 worker 领取任务；租约保障崩溃重投，共享去重挡重复" />

V3 最关键的是租约：领取后任务变成在途，确认成功才移除；没有这一层，崩溃就永久丢任务。

## 3. 三版架构差异

| 维度 | V1 单线程 | V2 并发 | V3 分布式 |
| --- | --- | --- | --- |
| 吞吐 | 受 RTT 限制，CPU 空等 | 等待重叠，接近 N 倍 | 加机器横向扩 |
| 去重 | 进程内 map | 加锁 seen | 共享 Redis 或 etcd |
| 限速 | 不需要 | 全局共享 ticker | 分布式令牌 |
| 状态位置 | 进程内存 | 进程内存 | 队列与去重外置 |
| 崩溃后果 | 从头再来 | 从头再来 | 租约超时重投 |
| 重复执行 | 不会 | 不会 | 可能，必须幂等 |
| 适用规模 | 几百页 | 几万页 | 千万页以上 |

V1 到 V2 的触发点：单线程等网络时 CPU 闲着，并发让等待重叠；代价是去重表加锁、限速必须全局。V2 到 V3 的触发点：状态全在进程内存，崩溃即全丢，单机资源也有上限；外置后消息可能重复投递，必须引入租约、确认与幂等键，见第 84 讲。演进不是免费的：V2 代价是锁竞争，V3 代价是至少一次语义。

## 4. 关键模块实现

### 4.1 V1 单线程：把基线立住

```go
package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
)

var linkRe = regexp.MustCompile(`href="(/[a-z0-9]*)"`)

// CrawlSingle 一个队列、一个循环，一次只处理一个 URL。
func CrawlSingle(base string, client *http.Client, max int) []string {
	seen := map[string]bool{}
	queue := []string{"/"}
	var visited []string
	for len(queue) > 0 && len(visited) < max {
		path := queue[0]
		queue = queue[1:]
		if seen[path] {
			continue
		}
		seen[path] = true
		resp, err := client.Get(base + path)
		if err != nil {
			continue // 单线程版：一页失败就跳过，不影响其它
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		visited = append(visited, path)
		for _, m := range linkRe.FindAllStringSubmatch(string(body), -1) {
			if !seen[m[1]] {
				queue = append(queue, m[1])
			}
		}
	}
	return visited
}

func main() {
	pages := map[string]string{
		"/":  `<a href="/a">a</a> <a href="/b">b</a> <a href="/c">c</a>`,
		"/a": `<a href="/d">d</a>`,
		"/b": `<a href="/d">d</a>`,
		"/c": `end`,
		"/d": `end`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	visited := CrawlSingle(srv.URL, srv.Client(), 10)
	fmt.Println("单线程访问顺序:", strings.Join(visited, " "), "总数=", len(visited))
}
```

这版没有并发，顺序稳定但抓一万个页面要串行走完千秒，瓶颈全在等待网络。

### 4.2 V2 并发：worker pool、去重与限速

```go
package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"time"
)

var linkRe = regexp.MustCompile(`href="(/[a-z0-9]*)"`)

// Pool 让 N 个 worker 共享 jobs，seen 在派发时就去重。
type Pool struct {
	client  *http.Client
	base    string
	mu      sync.Mutex
	seen    map[string]bool
	results []string
	jobs    chan string
	wg      sync.WaitGroup
	ticker  *time.Ticker
	limiter <-chan time.Time
	max     int
}

func NewPool(base string, client *http.Client, max int, rate time.Duration) *Pool {
	t := time.NewTicker(rate)
	return &Pool{
		client:  client,
		base:    base,
		seen:    make(map[string]bool),
		jobs:    make(chan string, 64),
		ticker:  t,
		limiter: t.C,
		max:     max,
	}
}

// enqueue 把「已调度」记入 seen，避免同一 URL 被多次入队。
func (p *Pool) enqueue(path string) {
	p.mu.Lock()
	if p.seen[path] || len(p.seen) >= p.max {
		p.mu.Unlock()
		return
	}
	p.seen[path] = true
	p.wg.Add(1)
	p.mu.Unlock()
	p.jobs <- path
}

func (p *Pool) worker() {
	for path := range p.jobs {
		<-p.limiter // 所有 worker 共享一个 ticker，实现全局限速
		resp, err := p.client.Get(p.base + path)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			p.mu.Lock()
			p.results = append(p.results, path)
			p.mu.Unlock()
			for _, m := range linkRe.FindAllStringSubmatch(string(body), -1) {
				p.enqueue(m[1])
			}
		}
		p.wg.Done()
	}
}

func (p *Pool) Run(seed string, workers int) []string {
	p.enqueue(seed)
	var wwg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wwg.Add(1)
		go func() {
			defer wwg.Done()
			p.worker()
		}()
	}
	// 所有任务完成后关闭 jobs，worker 才能退出 range。
	go func() {
		p.wg.Wait()
		close(p.jobs)
	}()
	wwg.Wait()
	p.ticker.Stop()
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.results...)
}

func main() {
	pages := map[string]string{
		"/":  `<a href="/a">a</a> <a href="/b">b</a> <a href="/c">c</a>`,
		"/a": `<a href="/d">d</a>`,
		"/b": `<a href="/d">d</a>`,
		"/c": `end`,
		"/d": `end`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	p := NewPool(srv.URL, srv.Client(), 5, time.Millisecond)
	visited := p.Run("/", 4)
	fmt.Println("并发版抓取数:", len(visited), "（顺序不固定）")
}
```

三个要点：seen 在派发时标记而非抓取完成时；发送 jobs 放在解锁之后避免持锁等待；关闭 jobs 由 wg 决定，Add 一定在对应 Done 之前。

### 4.3 V3 核心：租约、重投与幂等

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Task struct {
	URL string
}

type Lease struct {
	Task   Task
	ID     int
	Expiry time.Time
}

// Broker 是队列化分布式版的核心抽象：租约、确认、超时重投。
type Broker struct {
	mu       sync.Mutex
	pending  []Task
	inflight map[int]Lease
	nextID   int
	done     map[string]bool // 模拟共享去重表
}

func NewBroker() *Broker {
	return &Broker{
		inflight: make(map[int]Lease),
		done:     make(map[string]bool),
	}
}

func (b *Broker) Push(t Task) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done[t.URL] {
		return // 已完成的任务再次入队，幂等挡回
	}
	b.pending = append(b.pending, t)
}

func (b *Broker) Lease(ttl time.Duration) (Lease, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	// 先把超时未确认的租约放回队列，模拟 worker 崩溃后的故障转移。
	for id, l := range b.inflight {
		if now.After(l.Expiry) {
			delete(b.inflight, id)
			b.pending = append(b.pending, l.Task)
			fmt.Println("  租约超时，任务重投:", l.Task.URL)
		}
	}
	if len(b.pending) == 0 {
		return Lease{}, false
	}
	t := b.pending[0]
	b.pending = b.pending[1:]
	b.nextID++
	l := Lease{Task: t, ID: b.nextID, Expiry: now.Add(ttl)}
	b.inflight[l.ID] = l
	return l, true
}

func (b *Broker) Ack(l Lease) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.inflight[l.ID]; !ok {
		return false // 租约已过期被别人领走，这次确认作废
	}
	delete(b.inflight, l.ID)
	if b.done[l.Task.URL] {
		return false // 重复投递，靠去重表挡住
	}
	b.done[l.Task.URL] = true
	return true
}

func main() {
	b := NewBroker()
	b.Push(Task{"/a"})
	b.Push(Task{"/b"})
	b.Push(Task{"/a"}) // 重复入队，Push 层先去重
	fmt.Println("入队后待处理:", len(b.pending))

	l1, _ := b.Lease(20 * time.Millisecond)
	fmt.Println("worker1 领取:", l1.Task.URL, "（模拟崩溃，不 Ack）")

	time.Sleep(25 * time.Millisecond)
	for {
		l, ok := b.Lease(50 * time.Millisecond)
		if !ok {
			break
		}
		fmt.Printf("worker2 处理 %s 确认=%v\n", l.Task.URL, b.Ack(l))
	}
	fmt.Println("已完成任务数:", len(b.done))
}
```

这段跑完 V3 最易跳过的一条链：领取不确认、租约超时、重投、重新领取并确认。Ack 会检查租约是否仍在 inflight，迟到确认必须作废。

## 5. 失败路径与边界条件

进程崩溃 V1 与 V2 全丢，V3 可恢复；重复投递必然可能，需幂等键。V3 最贵的成本是从恰好一次退化成至少一次。租约 TTL 太短会因 GC 停顿频繁重投，太长则恢复慢。

<Callout type="warn" title="最贵的隐含成本" text="从恰好一次退化成至少一次，写库要用唯一键、发消息要用去重 ID，否则故障转移当天就会重复写入。" />

## 6. 测试与压测思路

三版跑同一站点断言抓到的 URL 集合相同；go test -race 跑 V2；V3 故意不 Ack 等租约过期断言重投，再补迟到 Ack 用例。

压测要量加速比随 worker 数的曲线；V3 额外监控队列深度与重投率。

## 7. 可继续扩展的方向

内存 Broker 换成 Kafka 或 NATS；用布隆过滤替换本地 map；任务交给第 110 讲调度系统。

## 随堂检测

<Quiz no="109-1" q="从 V2 演进到 V3 最根本的触发原因？" :options='["并发版代码太长","状态全在进程内存，崩溃即全丢，单机资源有上限","单线程太慢"]' answer="1" explain="外置队列与去重才获得可恢复与横向扩展能力。" />

## 一页纸速记

<KnowledgeCard :points='["V1 瓶颈是网络等待，V2 用并行重叠，V3 把状态外置","去重要在派发时标记，限速全局共享","V3 用租约重投，用幂等键消化重复投递"]' :pitfalls='["每 worker 一个 ticker 会把速率放大 N 倍","wg.Done 漏在失败分支会让 jobs 永不关闭","租约 TTL 太短会因 GC 停顿频繁重投"]' summary="演进每一步都是用新复杂度换上一版撑不住的指标。" />

## 小结与下一讲

- 同一业务三版对照：串行输在等待，并发输在状态单点，分布式赢在可恢复与可扩展。
- V2 关键是派发时去重、全局限速，以及 WaitGroup 与通道关闭的正确配合。
- V3 关键是租约重投与幂等，把恰好一次换成必须显式处理的至少一次。

[下一讲：实战 08 · 分布式任务调度系统](/projects/concurrent-scheduler)
