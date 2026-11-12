---
title: 第 116 讲 · 实战 10 · 秒杀与高并发库存
description: 流量削峰、限流排队、原子预扣杜绝超卖，讲清预扣与落库之间的最终一致
---

<StageHeader stage="L10 · 工程实战" level="★★★★★" time="80 分钟" goal="在极端流量下守住正确性" prereq="第 115 讲 · 电商微服务系统" />

# 第 116 讲 · 实战 10 · 秒杀与高并发库存

> 上一讲的库存扣减还只是一个普通函数调用，放在日常下单里没问题。但秒杀是另一种物理世界：库存 100 件，开抢瞬间涌进十万级请求，数据库扛不住，而且「先查余量再扣减」这种写法在并发下会把库存卖成负数。这一讲只围绕两条铁律展开：**绝不能超卖**，以及**预扣与落库之间如何做到最终一致**。为了守住它们，我们会主动接受「可以少卖」和「请求可以被拒绝」——这是秒杀架构里最核心的取舍。

## 这一讲你会得到什么

<CheckList id="L116-goal" title="本讲目标" :items='["复现先查后扣导致超卖的根因，并给出三种原子化改法","把检查与扣减合成一个临界区，配合幂等键杜绝重复扣减","用令牌桶限流加有界队列把瞬时洪峰摊平成匀速写库","设计预扣与落库之间的一致性兜底：回补、对账与超时释放"]' />

## 项目目标与约束

- **目标**：100 件库存、瞬时十万级 QPS 下，成功抢到的请求数**恒等于** 100；被拒绝的请求快速失败且不能压垮下游。
- **约束**：单 key 热点；允许少卖（少量已预扣因超时未支付而回补），**绝不允许超卖**；预扣在 Redis 之类的内存层完成，落库异步且最终一致。
- **明确取舍**：一致性上选「不超卖 + 最终一致」而不是「强一致 + 全成功」；可用性上选「限流快速失败」而不是「让所有请求都排队到超时」。

<Term word="超卖" text="卖出的数量超过实际库存，根因是「检查余量」与「扣减余量」之间没有被同一个原子操作包住" />

## 架构蓝图

<FlowTimeline direction="horizontal" :steps='[
  {"title":"静态与拦截","desc":"CDN 承载页面，网关挡掉明显无效请求","tone":"blue"},
  {"title":"限流","desc":"令牌桶按配额放行，超出直接拒绝","tone":"green"},
  {"title":"原子预扣","desc":"Redis 单条 Lua 或互斥锁内完成检查加扣减","tone":"orange"},
  {"title":"排队削峰","desc":"有界队列把洪峰摊平成匀速消费","tone":"orange"},
  {"title":"落库与对账","desc":"worker 写订单，定时对账并回补超时库存","tone":"red"}
]' />

<ConceptDiagram type="architecture-layer" title="秒杀链路分层：入口 → 限流 → 预扣 → 队列 → 落库 → 对账" caption="越靠前越要把请求挡掉，数据库永远只承受匀速的小流量" />

<ConceptDiagram type="gateway" title="网关在链路最前端做配额与削峰" caption="限流、校验、幂等键透传都在这一层完成" />

## 关键实现

以下代码同属 `package seckill`，分别为 `stock.go`、`limiter.go`、`queue.go`、`handler.go`、`stock_test.go`，同一目录 `go test ./...` 可编译运行。

### 1. 原子预扣：把检查与扣减合成一步

```go
// 文件：stock.go
package seckill

import (
	"errors"
	"sync"
)

var (
	ErrSoldOut       = errors.New("seckill: sold out")
	ErrNoReservation = errors.New("seckill: no reservation")
)

// Stock 用一把互斥锁把「检查 + 扣减」合成一个原子操作，
// 对应 Redis 里用一条 Lua 脚本或 DECR 命令，杜绝先查后扣造成的超卖。
type Stock struct {
	mu    sync.Mutex
	left  int
	dedup map[string]bool // 幂等：同一用户同一活动只成功一次
}

func NewStock(total int) *Stock {
	return &Stock{left: total, dedup: make(map[string]bool)}
}

// Deduct 返回是否本次新抢到。检查余量、幂等判定、扣减三步都在同一临界区内。
// 已受理的重复请求返回 ok=false、err=nil，由上层按幂等返回第一次的结果。
func (s *Stock) Deduct(userID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dedup[userID] {
		return false, nil
	}
	if s.left <= 0 {
		return false, ErrSoldOut
	}
	s.left--
	s.dedup[userID] = true
	return true, nil
}

// Refund 回补一份库存（队列满或超时未支付时调用）。
// 没有对应预扣记录时拒绝空回补，防止把库存凭空加回去。
func (s *Stock) Refund(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dedup[userID] {
		return ErrNoReservation
	}
	delete(s.dedup, userID)
	s.left++
	return nil
}

func (s *Stock) Left() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.left
}
```

### 2. 令牌桶限流

```go
// 文件：limiter.go
package seckill

import (
	"sync"
	"time"
)

// Limiter 是标准库实现的令牌桶，用于入口削峰：
// 超过容量的请求在网关层直接拒绝，不让它打到库存与数据库。
type Limiter struct {
	mu       sync.Mutex
	rate     float64 // 每秒补充令牌数
	capacity float64
	tokens   float64
	last     time.Time
}

func NewLimiter(rate, capacity float64) *Limiter {
	return &Limiter{rate: rate, capacity: capacity, tokens: capacity, last: time.Now()}
}

func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.tokens += now.Sub(l.last).Seconds() * l.rate
	if l.tokens > l.capacity {
		l.tokens = l.capacity
	}
	l.last = now
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}
```

### 3. 有界队列削峰

```go
// 文件：queue.go
package seckill

import (
	"context"
	"sync"
)

// OrderSink 抽象最终落库，把「内存预扣成功」与「异步写库」解耦。
type OrderSink interface {
	Save(ctx context.Context, userID string) error
}

// Queue 是有界排队器：入口限流后把请求放进队列，worker 匀速消费写库，
// 把瞬时洪峰摊平成数据库能承受的匀速写入，实现最终一致。
type Queue struct {
	ch   chan string
	sink OrderSink
	wg   sync.WaitGroup
}

func NewQueue(size, workers int, sink OrderSink) *Queue {
	q := &Queue{ch: make(chan string, size), sink: sink}
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}
	return q
}

func (q *Queue) worker() {
	defer q.wg.Done()
	for userID := range q.ch {
		// 落库失败要进入重试或对账，不能静默丢弃，
		// 否则会出现「预扣成功但没有订单」的不一致。
		_ = q.sink.Save(context.Background(), userID)
	}
}

// Enqueue 非阻塞入队：队列满说明下游已饱和，直接返回让上层回补并快速失败。
func (q *Queue) Enqueue(userID string) bool {
	select {
	case q.ch <- userID:
		return true
	default:
		return false
	}
}

func (q *Queue) Close() {
	close(q.ch)
	q.wg.Wait()
}
```

### 4. 网关串联：限流 → 预扣 → 入队

```go
// 文件：handler.go
package seckill

import (
	"encoding/json"
	"net/http"
)

type Handler struct {
	limit *Limiter
	stock *Stock
	queue *Queue
}

func NewHandler(l *Limiter, s *Stock, q *Queue) *Handler {
	return &Handler{limit: l, stock: s, queue: q}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !h.limit.Allow() {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	ok, err := h.stock.Deduct(req.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict) // 售罄
		return
	}
	if !ok {
		// 重复请求：幂等返回第一次的结果，不再扣减、不再入队。
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "already processed"})
		return
	}
	if !h.queue.Enqueue(req.UserID) {
		// 队列满必须回补，否则「预扣成功却没有订单」，形成少卖甚至资损。
		_ = h.stock.Refund(req.UserID)
		http.Error(w, "system busy", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
}
```

### 5. 并发不超卖测试与基准

```go
// 文件：stock_test.go
package seckill

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

func TestNoOversell(t *testing.T) {
	const total = 100
	const users = 1000
	s := NewStock(total)
	var success int64
	var wg sync.WaitGroup
	for i := 0; i < users; i++ {
		wg.Add(1)
		// 用参数传值，避免闭包捕获循环变量（第 15 讲的经典坑）。
		go func(id int) {
			defer wg.Done()
			ok, _ := s.Deduct("u" + strconv.Itoa(id))
			if ok {
				atomic.AddInt64(&success, 1)
			}
		}(i)
	}
	wg.Wait()
	if success != total {
		t.Fatalf("success=%d want=%d", success, total)
	}
	if s.Left() != 0 {
		t.Fatalf("left=%d", s.Left())
	}
}

func TestRefundIdempotent(t *testing.T) {
	s := NewStock(1)
	if ok, _ := s.Deduct("u1"); !ok {
		t.Fatal("want granted")
	}
	_ = s.Refund("u1")
	if err := s.Refund("u1"); err != ErrNoReservation {
		t.Fatalf("want ErrNoReservation, got %v", err)
	}
	if s.Left() != 1 {
		t.Fatalf("left=%d", s.Left())
	}
}

func BenchmarkDeduct(b *testing.B) {
	s := NewStock(1 << 30)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = s.Deduct("u" + strconv.Itoa(i))
	}
}
```

## 超卖的根因与三种原子化改法

超卖不是「并发太大」，而是**读与写之间有一道缝**。两个事务同时 `SELECT` 到余量 1，各自判断通过，再各自 `UPDATE` 减 1，最终余量变成负一。

<Compare title="扣减库存的两种写法" :panels='[
  {"label":"先查后扣（错误）","code":"SELECT stock; if stock 大于 0 then UPDATE stock=stock-1","note":"检查与扣减是两条语句，并发下都读到旧值，直接超卖"},
  {"label":"原子条件更新","code":"UPDATE stock SET stock=stock-1 WHERE sku=? AND stock 大于 0","note":"检查与扣减在同一语句内由行锁保证互斥，影响行数即为抢到数"}
]' />

| 方案 | 是否超卖 | 吞吐 | 复杂度 |
| --- | --- | --- | --- |
| 先查后扣 | 会 | 低 | 最低 |
| 数据库条件更新 | 不会 | 受限于数据库连接池 | 低 |
| Redis 单条 Lua / DECR | 不会 | 高，单线程原子 | 中，需处理与 DB 一致 |
| 分段库存（把 100 拆成 10 段各 10） | 不会 | 更高，摊开热点 | 高，需处理碎片 |

本讲 `Stock.Deduct` 用一把 `sync.Mutex` 模拟「单线程原子」，对应生产里 Redis 的一条 Lua 脚本。真实系统更常用 `DECR` 加判断返回值是否为负，或把库存分段到多个 key 上摊开热点。

<Term word="可少卖不可超卖" text="宁可因超时、取消、队列满而少成交几单并回补库存，也绝不让成交数超过真实库存，因为后者是资损与客诉" />

## 失败路径与边界条件

| 失败场景 | 后果 | 处理 |
| --- | --- | --- |
| 队列满 | 预扣成功却没订单 | `Enqueue` 失败立刻 `Refund` 回补 |
| 落库失败被静默丢弃 | 长期不一致 | 进重试队列 + 定时对账，不能只打日志 |
| 用户重复点击 | 重复扣减、重复下单 | 幂等键去重，重复请求返回首次结果 |
| 超时未支付 | 库存被永久占住 | 延迟任务释放预扣，`Refund` 幂等回补 |
| 热点单 key | 所有请求打同一分片 | 分段库存、本地库存 + 中央库存、请求合并 |
| 限流误杀 | 正常用户被拒 | 配额留余量、分层限流、灰度调参 |
| 回补竞态 | 回补与再次扣减交错 | 回补与扣减共用同一把锁 / 同一条 Lua |

<Callout type="warn" title="一致性缺口藏在「预扣」与「落库」之间" text="Redis 扣减成功后，到订单真正写进数据库之间有一段真空期：进程崩溃、MQ 抖动、worker 报错都会让这段永远接不上。必须靠「可靠消息 + 幂等消费 + 定时对账」把缺口补上，且对账要以库存为准、以回补为兜底，而不是假装消息一定送达。" />

## 深入一步：为什么单把锁仍是瓶颈

`sync.Mutex` 把所有 key 串行化，这在单机演示里够用，在生产里就是第 97 讲讲过的锁竞争拐点：并发越高，越多 goroutine 卡在同一把锁上，CPU 花在排队而非干活。分段库存的思路正是把一把锁拆成 N 把——和第 113 讲的锁分片、第 114 讲的分片路由是同一个思想在三个不同层面的复用。

## 测试与压测思路

- **不变量**：`TestNoOversell` 用 1000 个并发 goroutine 抢 100 件，断言成功数恰好等于 100 且余量为 0；这是本讲最重要的断言。
- **回补幂等**：`TestRefundIdempotent` 断言重复回补被 `ErrNoReservation` 挡住，库存不漂移。
- **竞态**：`go test -race` 覆盖 Deduct、Refund、Left 的并发调用。
- **全链路压测**：从限流配额、队列长度、worker 数三个维度逐步加压，观察 P99 延迟与「成功数是否恒等于库存数」；重点找队列被打满的拐点。
- **预案演练**：主动 kill worker、灌满队列、让落库返回错误，验证对账与回补能把状态收敛回来；同时演练关闭入口、库存冻结、降级为静态页。

## 工程实践 / 真实场景

生产秒杀通常把库存预热进 Redis，用一条 Lua 完成「判重 + 判余量 + 扣减 + 写预扣记录」，返回是否抢到；抢到的请求发可靠消息，由订单服务幂等消费；再加一个延迟队列处理 15 分钟未支付的订单，回补库存。页面与按钮尽量静态化并做答题、验证码等前置拦截，把无效流量挡在最外层。全链路要有 `trace_id` 与库存水位大盘，一旦水位异常立即冻结活动。

## 可继续扩展的方向

- 用 Redis Lua 或分段库存替换单机互斥锁，把热点摊开。
- 引入可靠消息（本地消息表 / 事务消息）与定时对账，补齐最终一致。
- 加延迟队列做超时未支付释放，并处理「已支付后释放」的竞态。
- 热点探测与动态限流、多维度配额（用户、IP、设备）。

## 动手练一练

<Exercise title="给回补加上超时释放" task="实现一个后台任务：预扣后 15 分钟未支付就自动回补库存，要求与「用户已支付」不产生竞态导致库存被错误加回。写出状态设计与关键判断。" :hints='["预扣记录要有状态：预留中、已支付、已回补","释放任务必须带条件：仅当状态仍为预留中才回补","判断与状态变更要原子，可用一条 Lua 或带版本号的更新"]' solution="预扣记录存 status 字段（RESERVED / PAID / RELEASED）；支付成功时把记录条件更新为 PAID（仅当当前是 RESERVED）；延迟任务到点时执行「仅当 status 仍为 RESERVED 时置为 RELEASED 并 left 加一」的条件更新，PAID 状态会让这一步影响 0 行从而不回补；所有判断与更新包在同一临界区或同一条 Lua 里。测试覆盖「支付先到」「释放先到」「两者并发」三种时序。" explain="竞态的本质是「读状态」与「改状态」之间又有了一道缝，和第 5 节超卖是同一类问题；用条件更新把判断与变更合为一步，是通用解法。" />

## 随堂检测

<Quiz no="116-1" q="先查后扣为什么会超卖？" :options='["数据库不支持事务","检查余量与扣减余量之间没有被同一个原子操作包住，并发都读到旧值","Redis 太慢","因为没加索引"]' answer="1" explain="根因是 read-modify-write 中间的缝。两个事务并发读到同一个旧余量，各自判断通过后分别扣减，于是合计扣多了。把检查与扣减合并成一条条件更新即可解决。" />

<Predict no="116-2" q="库存 100，1000 个并发 Deduct，最终成功数与余量是多少？" :options='["成功 1000，余量负 900","成功 100，余量 0","成功约 100 上下浮动，余量约 0","成功 1，余量 99"]' answer="1" output="成功 100，余量 0" explain="检查、判重、扣减三步在同一把锁内完成，形成了严格的串行序列，因此恰好前 100 个成功，其余全部拿到 ErrSoldOut，余量精确为 0，不会浮动。" />

<Quiz no="116-3" q="预扣成功但落库失败，正确的兜底是哪一种？" :options='["忽略它，用户会重新下单","把预扣记进可靠消息并幂等消费，配合定时对账与回补","直接把库存加回去，让用户重试","把错误写进日志就完事"]' answer="1" explain="预扣与落库之间是一段一致性真空。必须用可靠消息保证至少投递一次、消费幂等，再用对账兜底；直接回补可能和已支付请求竞态，只写日志则永久不一致。" />

## 一页纸速记

<KnowledgeCard :points='["超卖根因是检查与扣减之间有缝，用一条原子操作把两者合起来","可少卖不可超卖：队列满、超时未支付都回补，成交数绝不超库存","限流在最前挡流量，有界队列把洪峰摊平成匀速写库","幂等键贯穿全链路，重复请求返回首次结果而非重复扣减","预扣与落库之间靠可靠消息、幂等消费、定时对账兜底"]' :pitfalls='["先查后扣，把库存卖成负数","Enqueue 失败忘了 Refund，出现预扣成功却没订单","落库失败只打日志，不做重试与对账","回补不做条件判断，和已支付竞态把库存凭空加回"]' summary="秒杀的正确性不靠更高的并发，而靠把每一处「读后再写」都收进一个原子步骤。" />

## 小结与下一讲

1. 超卖的根因是 read-modify-write 之间的缝，用互斥锁（生产用 Redis Lua / 条件更新）把检查与扣减合为一步，并用幂等键去重。
2. 令牌桶在入口挡流量、有界队列把洪峰摊平，队列满或超时则 `Refund` 回补，守住「可少卖不可超卖」。
3. 预扣与落库之间的真空期必须靠可靠消息、幂等消费与定时对账收敛，压测与预案要覆盖 worker 崩溃、队列打满、落库报错三类故障。

到这里，业务系统本身的高并发正确性已经讲透。剩下的挑战是「怎么把这么多 Go 服务稳定地管起来」——声明式地描述期望状态、让控制器自动收敛、把指标日志告警收成一套平台。下一讲用 Go 写一个 Kubernetes Operator。

[下一讲：实战 11 · Kubernetes Operator 与可观测平台](/projects/cloud-operator)
