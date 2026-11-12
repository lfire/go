---
title: 第 115 讲 · 实战 09 · 电商微服务系统
description: 按业务边界拆分服务、用 .proto 定契约、以 Saga 编排下单与补偿、用 HTTP 网关统一入口
---

<StageHeader stage="L10 · 工程实战" level="★★★★☆" time="90 分钟" goal="走完一个企业级系统的全流程" prereq="第 114 讲 · 分片、复制与 Raft" />

# 第 115 讲 · 实战 09 · 电商微服务系统

> 上一讲结尾留了一个尖锐的问题：分片 KV 单 key 原子，可真实业务里「下一个订单」偏偏要同时动订单、库存、支付三处数据。把这三件事塞进一个单体并不难，难的是当它们要各自独立部署、独立扩容、由不同团队维护时，怎么保证一次下单要么全成、要么全不留痕。这一讲正面处理**服务拆分**与**跨服务一致性**：用契约把边界钉死，用 Saga 编排补偿，用网关统一入口。这里不追求分布式强事务，而是诚实接受最终一致。

## 这一讲你会得到什么

<CheckList id="L115-goal" title="本讲目标" :items='["按业务边界而不是技术层拆分服务，并说明拆分代价","用依赖倒置的接口隔离库存、支付、订单，做到可替换与可 mock","写出带逆序补偿的 Saga 编排器，处理幂等与空补偿","用 HTTP 网关透传幂等键、做超时与错误映射"]' />

## 项目目标与约束

- **目标**：把电商系统拆成用户、商品、订单、库存、支付等服务；下单链路跨订单、库存、支付三处；对外提供一个 HTTP JSON 入口。
- **约束**：每个服务独库、独立部署；服务间契约用 `.proto` 定义、内部走 gRPC；**不要求跨服务强事务**，只要求最终一致；所有写操作必须幂等。
- **明确边界**：本讲聚焦「拆分 + 一致性」，库存扣减本身的高并发正确性（超卖）留给下一讲；服务发现、链路追踪、熔断沿用第 75、80、79 讲。

<Term word="Saga" text="把长事务拆成一串本地事务，每步配一个补偿操作；任一步失败就逆序执行已成功步骤的补偿，达到最终一致" />

## 架构蓝图

<ConceptDiagram type="architecture-layer" title="电商微服务分层：网关 → 订单编排 → 库存 / 支付 / 商品 → 各自独库" caption="拆分的判据是业务边界与数据所有权，不是 controller / service / dao 的技术层" />

<ConceptDiagram type="saga" title="下单 Saga：预扣库存 → 建订单 → 支付 → 确认，失败时逆序补偿" caption="补偿必须与正向操作一一对应，且自身幂等" />

<FlowTimeline direction="horizontal" :steps='[
  {"title":"网关闭口","desc":"校验参数、透传幂等键、限流","tone":"blue"},
  {"title":"预扣库存","desc":"Reserve 记预留，可释放","tone":"green"},
  {"title":"建订单","desc":"落库，状态 CREATED","tone":"green"},
  {"title":"支付","desc":"Charge 扣款，可退款","tone":"orange"},
  {"title":"确认","desc":"订单置 PAID，链路完成","tone":"red"}
]' />

## 关键实现

以下代码同属 `package ecom`，分别为 `contract.go`、`inventory.go`、`saga.go`、`gateway.go`、`saga_test.go`，同一目录 `go test ./...` 可编译运行。

### 1. 契约：领域模型与依赖接口

```go
// 文件：contract.go
package ecom

import (
	"errors"
	"time"
)

var (
	ErrStockNotEnough = errors.New("ecom: stock not enough")
	ErrOrderNotFound  = errors.New("ecom: order not found")
	ErrDuplicate      = errors.New("ecom: duplicate request")
	ErrBadQty         = errors.New("ecom: bad qty")
	ErrSagaAbort      = errors.New("ecom: saga aborted")
)

// 幂等键贯穿整条链路：网关透传，各服务用同一键去重。
type Order struct {
	ID        string
	UserID    string
	SKU       string
	Qty       int
	Status    string
	IdemKey   string
	CreatedAt time.Time
}

// Inventory 是订单服务对库存服务的依赖。用接口而非具体类型，
// 既能换成 gRPC 客户端，也能在单测里塞 fake，这是依赖倒置。
type Inventory interface {
	Reserve(idemKey, sku string, qty int) error
	Release(idemKey, sku string, qty int) error
}

// Payment 是支付服务依赖。
type Payment interface {
	Charge(idemKey, orderID string, amount int64) error
	Refund(idemKey, orderID string) error
}

// OrderStore 只负责订单落库。
type OrderStore interface {
	Create(o Order) error
	UpdateStatus(orderID, status string) error
}
```

### 2. 库存服务：幂等预扣

```go
// 文件：inventory.go
package ecom

import "sync"

var _ Inventory = (*MemInventory)(nil)

type reserveRec struct {
	sku string
	qty int
}

// MemInventory 演示库存服务的幂等预扣：同一 idemKey 只扣一次。
// reserved 兼作去重表；释放时若预扣从未成功则不做空回补，避免凭空增加库存。
type MemInventory struct {
	mu       sync.Mutex
	stock    map[string]int
	reserved map[string]reserveRec
}

func NewMemInventory(initial map[string]int) *MemInventory {
	copied := make(map[string]int, len(initial))
	for k, v := range initial {
		copied[k] = v
	}
	return &MemInventory{stock: copied, reserved: make(map[string]reserveRec)}
}

func (m *MemInventory) Reserve(idemKey, sku string, qty int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.reserved[idemKey]; ok {
		return nil // 幂等：重复请求直接成功，不再扣减
	}
	if qty <= 0 {
		return ErrBadQty
	}
	if m.stock[sku] < qty {
		return ErrStockNotEnough
	}
	m.stock[sku] -= qty
	m.reserved[idemKey] = reserveRec{sku: sku, qty: qty}
	return nil
}

func (m *MemInventory) Release(idemKey, _ string, _ int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.reserved[idemKey]
	if !ok {
		return nil // 空补偿：预扣从未成功，防悬挂补偿
	}
	m.stock[rec.sku] += rec.qty
	delete(m.reserved, idemKey)
	return nil
}
```

### 3. Saga 编排器：正向 + 逆序补偿

```go
// 文件：saga.go
package ecom

import "fmt"

// step 是 Saga 的一步：do 正向操作，undo 补偿操作（失败时逆序执行）。
type step struct {
	name string
	do   func() error
	undo func()
}

type Orchestrator struct {
	inv   Inventory
	pay   Payment
	store OrderStore
}

func NewOrchestrator(inv Inventory, pay Payment, store OrderStore) *Orchestrator {
	return &Orchestrator{inv: inv, pay: pay, store: store}
}

// CreateOrder 以 Saga 编排下单：任一步失败，逆序执行已成功步骤的补偿。
// 所有副作用都放进 steps，最后一步也是可补偿的一步，避免留下半截状态。
func (o *Orchestrator) CreateOrder(ord Order) error {
	var done []step
	rollback := func() {
		for i := len(done) - 1; i >= 0; i-- {
			done[i].undo()
		}
	}
	steps := []step{
		{name: "reserve", do: func() error {
			return o.inv.Reserve(ord.IdemKey, ord.SKU, ord.Qty)
		}, undo: func() {
			_ = o.inv.Release(ord.IdemKey, ord.SKU, ord.Qty)
		}},
		{name: "create", do: func() error {
			ord.Status = "CREATED"
			return o.store.Create(ord)
		}, undo: func() {
			_ = o.store.UpdateStatus(ord.ID, "CANCELED")
		}},
		{name: "charge", do: func() error {
			return o.pay.Charge(ord.IdemKey, ord.ID, int64(ord.Qty)*100)
		}, undo: func() {
			_ = o.pay.Refund(ord.IdemKey, ord.ID)
		}},
		{name: "finalize", do: func() error {
			return o.store.UpdateStatus(ord.ID, "PAID")
		}, undo: func() {
			_ = o.store.UpdateStatus(ord.ID, "CANCELED")
		}},
	}
	for _, s := range steps {
		if err := s.do(); err != nil {
			rollback()
			return fmt.Errorf("%w at %s: %v", ErrSagaAbort, s.name, err)
		}
		done = append(done, s)
	}
	return nil
}
```

### 4. HTTP 网关：REST 转内部编排

```go
// 文件：gateway.go
package ecom

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type createReq struct {
	UserID  string `json:"user_id"`
	SKU     string `json:"sku"`
	Qty     int    `json:"qty"`
	IdemKey string `json:"idem_key"`
}

// Gateway 把 HTTP JSON 请求转成对内部编排器的调用，统一处理幂等键、超时与错误映射。
type Gateway struct {
	orch *Orchestrator
	next func() string // 注入订单 ID 生成器，便于测试
}

func NewGateway(o *Orchestrator, nextID func() string) *Gateway {
	return &Gateway{orch: o, next: nextID}
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req createReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.IdemKey == "" || req.Qty <= 0 {
		http.Error(w, "idem_key and qty required", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	ord := Order{
		ID: g.next(), UserID: req.UserID, SKU: req.SKU,
		Qty: req.Qty, IdemKey: req.IdemKey, CreatedAt: time.Now(),
	}
	// 用 goroutine 承载编排，select 监听超时；真正取消下游还需 context 透传。
	errCh := make(chan error, 1)
	go func() { errCh <- g.orch.CreateOrder(ord) }()
	select {
	case <-ctx.Done():
		http.Error(w, "timeout", http.StatusGatewayTimeout)
	case err := <-errCh:
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"order_id": ord.ID, "status": "PAID"})
	}
}
```

### 5. 表驱动测试：补偿与幂等

```go
// 文件：saga_test.go
package ecom

import (
	"errors"
	"testing"
)

type fakeInv struct {
	failReserve bool
	released    int
}

func (f *fakeInv) Reserve(_, _ string, q int) error {
	if f.failReserve {
		return errors.New("no stock")
	}
	return nil
}
func (f *fakeInv) Release(_, _ string, q int) error {
	f.released += q
	return nil
}

type fakePay struct {
	failCharge bool
	refunds    int
}

func (f *fakePay) Charge(_, _ string, _ int64) error {
	if f.failCharge {
		return errors.New("payment declined")
	}
	return nil
}
func (f *fakePay) Refund(_, _ string) error { f.refunds++; return nil }

type fakeStore struct{ last string }

func (f *fakeStore) Create(o Order) error            { f.last = o.Status; return nil }
func (f *fakeStore) UpdateStatus(_, s string) error  { f.last = s; return nil }

func TestSagaCompensation(t *testing.T) {
	cases := []struct {
		name        string
		failReserve bool
		failCharge  bool
		wantRelease int
		wantCancel  bool
	}{
		{"库存不足直接失败", true, false, 0, false},
		{"支付失败需释放库存", false, true, 2, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inv := &fakeInv{failReserve: c.failReserve}
			pay := &fakePay{failCharge: c.failCharge}
			store := &fakeStore{}
			o := NewOrchestrator(inv, pay, store)
			err := o.CreateOrder(Order{ID: "o1", SKU: "s1", Qty: 2, IdemKey: "k1"})
			if err == nil {
				t.Fatal("want saga abort")
			}
			if inv.released != c.wantRelease {
				t.Fatalf("released=%d want=%d", inv.released, c.wantRelease)
			}
			if (store.last == "CANCELED") != c.wantCancel {
				t.Fatalf("last status=%s", store.last)
			}
		})
	}
}

func TestInventoryIdempotent(t *testing.T) {
	inv := NewMemInventory(map[string]int{"s1": 5})
	_ = inv.Reserve("k1", "s1", 2)
	_ = inv.Reserve("k1", "s1", 2) // 重复幂等键不应再扣
	_ = inv.Release("k1", "s1", 2)
	_ = inv.Release("k1", "s1", 2) // 重复释放不应回补两次
	inv.mu.Lock()
	got := inv.stock["s1"]
	inv.mu.Unlock()
	if got != 5 {
		t.Fatalf("stock=%d want 5", got)
	}
}
```

## 为什么这样拆分：服务边界的判据

拆分不是把代码目录切开，而是**划分数据所有权**。订单服务拥有订单表、库存服务拥有库存表，任何跨服务读写都必须走契约，不能偷连对方数据库。

<Compare title="两种拆法的后果" :panels='[
  {"label":"按技术层拆","code":"controller / service / dao 三个服务","note":"一次下单跨三服务，还共享一个库，等于没拆且多了两次网络调用"},
  {"label":"按业务边界拆","code":"用户 / 商品 / 订单 / 库存 / 支付","note":"各服务有自己的数据与契约，可独立部署扩容，代价是分布式事务"}
]' />

<CodeTabs title="契约优先：先定 .proto，再落 Go 接口" :tabs='[{"label":"proto 契约","code":"service OrderService { rpc CreateOrder(CreateOrderReq) returns (CreateOrderResp); }","note":"字段与状态码是跨语言、跨团队的唯一事实来源"},{"label":"Go 依赖接口","code":"type Inventory interface { Reserve(idemKey, sku string, qty int) error; Release(idemKey, sku string, qty int) error }","note":"订单服务只依赖这张窄接口，gRPC 实现与 fake 可互换"}]' />

拆分的代价必须说清：一次下单从「一个本地事务」变成「四次远程调用 + 一串补偿」，延迟与失败面都变大了；换来的是各服务独立伸缩、独立发布、故障隔离。没有这个收益，就不该拆。

## 失败路径与边界条件

| 失败场景 | 后果 | 处理 |
| --- | --- | --- |
| 支付失败 | 库存已预扣、订单已建 | 逆序补偿：取消订单 + 释放库存 |
| 补偿请求重复到达 | 库存被回补两次 | 补偿以幂等键去重，`MemInventory` 用 `reserved` 表拦截 |
| 补偿先于正向到达（悬挂） | 释放了一个不存在的预留 | `Release` 查不到记录即空操作，不做空回补 |
| 网关超时 | 客户端收到 504，编排仍在后台跑 | 需把 `context` 透传到编排器，否则只是「假超时」 |
| 订单状态非法跃迁 | PAID 又被改回 CREATED | 状态机 + 乐观锁（版本号）约束 |
| 补偿本身失败 | 链路卡在中间态 | 补偿进重试队列 + 定时对账，不能静默丢弃 |
| 消息 / 通知丢失 | 库存与订单长期不一致 | 本地消息表或事务消息兜底 |

<Callout type="warn" title="Saga 最容易错的三处" text="一是补偿不幂等，重试一次就多退一次款；二是空补偿与悬挂补偿没挡住，凭空把库存加回去；三是把超时当成取消——网关 select 到 ctx.Done 就返回了，可编排 goroutine 还在跑，必须让 context 一路传到每个下游调用。" />

## 测试与压测思路

- **补偿正确性**：`TestSagaCompensation` 用表驱动覆盖「第一步就失败」与「中途失败」两类，断言释放量与订单终态。
- **幂等**：`TestInventoryIdempotent` 重复 Reserve / Release，断言库存不漂移。
- **契约测试**：用 `httptest` 起网关与假上游，验证请求/响应字段兼容；`.proto` 变更走破坏性检查。
- **故障注入**：让支付服务随机超时或返回失败，跑上千次下单，断言「成功订单数 + 已退款数 = 支付调用数」且库存守恒。
- **全链路压测**：逐步加压观察编排延迟分位，找库存服务与订单库的拐点；联调时务必带上 `trace_id` 贯穿（第 80 讲）。

## 工程实践 / 真实场景

生产系统会给 Saga 配一个持久化状态机：每步执行前先记「进行中」，成功或补偿后落终态，进程崩溃后靠定时任务扫描中间态继续推进，而不是依赖某次内存里的调用栈。订单、库存、支付还会各自接入消息队列做异步化与削峰，搜索侧通过订阅 binlog 同步商品索引。这一层做扎实了，才轮到下一讲去扛秒杀的极端流量。

## 可继续扩展的方向

- 把内存编排器换成持久化 Saga 状态机 + 可靠消息队列，支持崩溃续跑。
- 引入 TCC 或事务消息，在需要更强一致的场景替代补偿式 Saga。
- 加服务发现、熔断、链路追踪与统一错误码（第 75、79、80、57 讲）。
- 把库存扣减换成 Redis 原子预扣，应对秒杀——正是下一讲的主题。

## 动手练一练

<Exercise title="让补偿失败也能收敛" task="当前 undo 里的错误被下划线忽略了。请设计一种机制，使补偿失败不会导致链路永久卡在中间态，并写出关键结构与测试思路。" :hints='["补偿不能只调用一次就丢，需要一个可重试的记录","建议把「待补偿」持久化成一条带状态的记录","重试要有退避，且补偿本身必须幂等"]' solution="定义 Compensation{Step, IdemKey, Payload, Done} 记录，CreateOrder 回滚时先把每条待补偿写入 durable 表再尝试执行；后台 worker 用带退避的循环扫描 Done=false 的记录，执行幂等 undo 成功后置 Done=true。测试里让第一次 Release 返回错误、第二次成功，断言 worker 重试后库存最终回补且只回补一次。" explain="Saga 的可靠性不在正向流程，而在「补偿一定能做完」。把补偿持久化 + 幂等重试，是从「能跑通的 demo」走向生产可用的分水岭。" />

## 随堂检测

<Quiz no="115-1" q="拆分微服务最可靠的判据是什么？" :options='["按 controller / service / dao 分层","按代码行数平均切分","按业务边界与数据所有权划分","按团队人数一人一个服务"]' answer="2" explain="数据所有权决定边界：每个服务独占自己的库，跨服务只能走契约。按技术层拆会共享同一个库，既没获得隔离性又平白多出网络调用。" />

<Predict no="115-2" q="支付步骤失败后，Saga 的补偿按什么顺序执行？" :options='["按正向顺序：预扣、建单、支付依次撤销","逆序：先撤销建单，再释放预扣","只撤销失败的那一步","随机顺序，最终一致即可"]' answer="1" output="逆序：先撤销建单，再释放预扣" explain="补偿必须逆序：后发生的依赖先发生的，先把订单取消再把库存放回去，才不会在短时间内出现「有库存但没有对应订单」之外的更乱状态。" />

<Quiz no="115-3" q="为什么说 Saga 只能保证最终一致，而不是强一致？" :options='["因为它用了 gRPC","因为正向与补偿之间，系统会短暂处于中间态且对各服务可见","因为它没用数据库","因为补偿一定失败"]' answer="1" explain="Saga 由多个独立的本地事务组成，步骤之间没有其他事务隔离，中间状态对外可见；它靠补偿把系统收敛到一致，属于 BASE 而非 ACID。" />

## 一页纸速记

<KnowledgeCard :points='["按业务边界与数据所有权拆分，跨服务只走契约，不偷连对方库","依赖倒置：订单服务只依赖 Inventory / Payment / OrderStore 三张窄接口","Saga = 正向步骤加一一对应的补偿，失败逆序回滚","幂等键贯穿全链路，补偿要防重复、防空补偿、防悬挂","超时必须透传 context，否则只是客户端先返回，后台仍在跑"]' :pitfalls='["按技术层拆分还共享一个库，等于白拆","补偿不幂等，重试一次多退一次款","把网关超时当成下游取消，留下幽灵订单","忽略补偿失败，链路永久卡在中间态"]' summary="拆分的收益是独立伸缩与故障隔离，代价是必须自己用契约、幂等与补偿把一致性重新建起来。" />

## 小结与下一讲

1. 服务按业务边界与数据所有权拆分，契约用 `.proto` 优先定义，依赖用窄接口倒置，随时可换实现或塞 fake。
2. `Orchestrator` 用「正向步骤 + 一一对应的补偿」实现 Saga，失败逆序回滚；库存服务用幂等键去重并挡住空补偿与悬挂补偿。
3. 网关统一校验、透传幂等键、映射错误，同时我们看清了「超时不等于取消」「补偿会失败」两处必须靠持久化状态机与重试队列兜底。

这一讲解决的是「跨服务一致性」，但库存扣减仍是一个普通的函数调用。到了秒杀那种瞬时十万级请求，先查再扣的写法会直接把库存卖成负数。下一讲专门解决超卖与流量削峰。

[下一讲：实战 10 · 秒杀与高并发库存](/projects/micro-seckill)
