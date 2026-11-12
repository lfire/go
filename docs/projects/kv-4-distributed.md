---
title: 第 114 讲 · KV 贯穿线 ④ 分片、复制与 Raft
description: 把单机 KV 演进成分布式存储：分片路由、多副本、Raft 日志提交与多数派、读写一致性
---

<StageHeader stage="L10 · 工程实战" level="★★★★★" time="90 分钟" goal="把 KV 演进成分布式存储" prereq="第 113 讲 · WAL、持久化与并发控制" />

# 第 114 讲 · KV 贯穿线 ④ 分片、复制与 Raft

> 三讲走下来，我们已经有了一份稳定契约和三种实现：内存、TCP 客户端、带 WAL 的持久化版。但它们都跑在**一台机器**上——机器宕机服务就停，容量顶死在一台机器的内存与磁盘上。这一讲做最后一次跃迁：用**分片**把数据摊到多台机器解决容量，用**副本**解决单点，用 **Raft** 让多个副本对同一串写命令达成一致。这是第 111 讲那块「窄腰」能承载的最大一次变化，而 `Apply(Command)` 依然一个字不改。

## 这一讲你会得到什么

<CheckList id="L114-goal" title="本讲目标" :items='["用 Router 把 Command 按 key 路由到唯一分片，且仍实现 Storage","理解 Raft 为何用「日志 + 多数派」而不是「同步写所有副本」","写出可编译的最小日志提交内核，讲清任期与提交索引","分析脑裂、只读旧值、扩容迁移、跨分片事务四类失败路径"]' />

## 项目目标与约束

「上一版遇到了什么问题，所以这一版加了什么」：第 113 讲的 `WALStorage` 有单点故障，且所有 key 仍在一台机器上；把 WAL 拆到多机又立刻引出「同一份数据谁说了算」。所以这一版加三样东西。

- **目标**：水平扩展容量（分片）、容忍节点故障（副本）、对写顺序达成一致（Raft），对外保持单 key 的线性一致读写。
- **约束**：**只保证单个 key 的原子性与一致性，跨 key、跨分片不提供事务**——这是最重要也最容易被忽略的取舍。副本数取奇数，写需多数派。
- **诚实边界**：下面的 `raftCore` 是「日志复制 + 多数派提交」的**最小可编译内核**，用来讲清一致性从哪来；选举、心跳、日志持久化、网络传输交给 `etcd/raft` 这类成熟库，教程刻意不贴整个仓库。

<Term word="多数派" text="quorum，写只需超过半数副本确认即可提交，从而在允许少数节点故障的同时避免脑裂" />

## 架构蓝图

<ConceptDiagram type="raft" title="Raft 日志复制：Leader 追加日志，多数派确认后提交并应用" caption="写不是同步刷到所有副本，而是等到多数派即可返回" />

<ConceptDiagram type="hash-ring" title="分片路由：key 经哈希环定位到唯一副本组" caption="每个分片是一个独立 Raft 组，组内保持一致，组间只做路由" />

<FlowTimeline direction="horizontal" :steps='[
  {"title":"① 内存版","desc":"定 Command 与 Storage 契约","tone":"blue"},
  {"title":"② 网络层","desc":"Client 也实现 Storage","tone":"green"},
  {"title":"③ 持久化","desc":"WAL、恢复、锁分片、快照","tone":"orange"},
  {"title":"④ 分布式","desc":"Router 分片、副本组、Raft 提交","tone":"red"}
]' />

## 关键实现

以下代码同属 `package kv`，分别为 `contract.go`（契约快照，仅增加两个分布式错误）、`router.go`、`raft.go`、`replica.go`、`cluster_test.go`，同一目录 `go test ./...` 可编译运行。

### 1. 契约快照（新增分布式错误）

```go
// 文件：contract.go
package kv

import "errors"

type Op byte

const (
	OpGet Op = 1
	OpSet Op = 2
	OpDel Op = 3
)

var (
	ErrNotFound  = errors.New("kv: key not found")
	ErrClosed    = errors.New("kv: storage closed")
	ErrEmptyKey  = errors.New("kv: empty key")
	ErrBadOp     = errors.New("kv: unknown op")
	ErrNotLeader = errors.New("kv: not leader")     // 写或强一致读打到了 follower
	ErrNoQuorum  = errors.New("kv: write lost quorum") // 多数派未达成，写不能提交
)

type Command struct {
	Op    Op
	Key   string
	Value []byte
}

type Result struct {
	Value []byte
	Found bool
}

type Storage interface {
	Apply(cmd Command) (Result, error)
	Close() error
}
```

### 2. 分片路由：Router 也是 Storage

```go
// 文件：router.go
package kv

import "hash/fnv"

// Group 抽象一个分片对应的副本组；真实实现里它就是 Raft 组。
// 用接口隔离，路由层不依赖任何具体共识库。
type Group interface {
	Storage
	ShardID() int
}

// 编译期断言：分布式入口仍满足最初的契约。
var _ Storage = (*Router)(nil)

// Router 按 key 把命令路由到唯一分片，groups 的长度即分片数。
type Router struct {
	groups []Group
}

func NewRouter(groups []Group) *Router {
	return &Router{groups: groups}
}

func (r *Router) groupOf(key string) Group {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	// 先取模再转 int，避免 32 位平台上 uint32 转 int 变负导致越界。
	return r.groups[h.Sum32()%uint32(len(r.groups))]
}

func (r *Router) Apply(cmd Command) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	return r.groupOf(cmd.Key).Apply(cmd)
}

func (r *Router) Close() error {
	var first error
	for _, g := range r.groups {
		if err := g.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
```

### 3. Raft 最小内核：日志复制与多数派提交

```go
// 文件：raft.go
package kv

import "sync"

// LogEntry 是一条待复制的日志，提交后才允许应用到状态机。
type LogEntry struct {
	Term uint64
	Cmd  Command
}

// raftCore 只保留「写先记日志、多数确认再提交」这一条主线。
// 生产环境由 etcd/raft 负责选举、心跳、持久化与网络；这里用可编译的
// 内存内核说明一致性来源：所有副本按同一顺序应用同一串命令。
type raftCore struct {
	mu          sync.Mutex
	term        uint64
	log         []LogEntry
	commitIndex int
	peers       int            // 含自身的节点总数，取奇数
	acks        []int          // 每条日志已收到的确认数
	apply       func(Command)  // 提交后应用到状态机的回调
}

func newRaftCore(peers int, apply func(Command)) *raftCore {
	return &raftCore{peers: peers, apply: apply, term: 1}
}

// propose 由 leader 调用：追加日志并先给自己记一票，返回日志下标。
func (r *raftCore) propose(cmd Command) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = append(r.log, LogEntry{Term: r.term, Cmd: cmd})
	r.acks = append(r.acks, 1)
	return len(r.log) - 1
}

// ack 由某个副本确认复制成功时调用；达到多数派就推进提交索引并应用。
func (r *raftCore) ack(index int) {
	r.mu.Lock()
	if index < 0 || index >= len(r.acks) || index < r.commitIndex {
		r.mu.Unlock()
		return
	}
	r.acks[index]++
	var pending []Command
	if r.acks[index] > r.peers/2 && index >= r.commitIndex {
		start := r.commitIndex
		for i := start; i <= index; i++ {
			pending = append(pending, r.log[i].Cmd)
		}
		r.commitIndex = index + 1
	}
	r.mu.Unlock()
	// apply 放在锁外，避免状态机回调再次取 raftCore.mu 造成自锁。
	for _, c := range pending {
		r.apply(c)
	}
}

func (r *raftCore) isCommitted(index int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return index < r.commitIndex
}
```

### 4. 副本组：只有 leader 接受写

```go
// 文件：replica.go
package kv

import "sync"

var _ Group = (*ReplicaGroup)(nil)

// ReplicaGroup 是一个分片的多副本组：只有 leader 接受写，
// 写先经 raftCore 复制到多数派，提交后才落到本地状态机。
type ReplicaGroup struct {
	id     int
	leader bool
	core   *raftCore
	mu     sync.RWMutex
	data   map[string][]byte
}

func NewReplicaGroup(id, peers int, leader bool) *ReplicaGroup {
	g := &ReplicaGroup{id: id, leader: leader, data: make(map[string][]byte)}
	// 提交回调：把命令应用到本地 map，幂等，重放安全。
	g.core = newRaftCore(peers, func(cmd Command) {
		g.mu.Lock()
		defer g.mu.Unlock()
		switch cmd.Op {
		case OpSet:
			g.data[cmd.Key] = append([]byte(nil), cmd.Value...)
		case OpDel:
			delete(g.data, cmd.Key)
		}
	})
	return g
}

func (g *ReplicaGroup) ShardID() int { return g.id }

// Replicate 模拟 leader 收到一个 follower 的确认；真实里是 AppendEntries 的响应。
func (g *ReplicaGroup) Replicate(index int) { g.core.ack(index) }

func (g *ReplicaGroup) Apply(cmd Command) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	// 强一致读走 leader（或用 ReadIndex / lease read），避免读到未提交的旧值。
	if !g.leader {
		return Result{}, ErrNotLeader
	}
	if cmd.Op == OpGet {
		g.mu.RLock()
		defer g.mu.RUnlock()
		v, ok := g.data[cmd.Key]
		if !ok {
			return Result{}, ErrNotFound
		}
		return Result{Value: append([]byte(nil), v...), Found: true}, nil
	}
	if cmd.Op != OpSet && cmd.Op != OpDel {
		return Result{}, ErrBadOp
	}
	idx := g.core.propose(cmd)
	// 真实系统在这里并行发 AppendEntries 并等待多数派；本例用同步 ack
	// 演示提交条件：达到多数派才 apply，压掉了网络与选举的复杂度。
	for i := 1; i < g.core.peers; i++ {
		g.core.ack(idx)
	}
	if !g.core.isCommitted(idx) {
		return Result{}, ErrNoQuorum
	}
	return Result{Found: true}, nil
}

func (g *ReplicaGroup) Close() error { return nil }
```

### 5. 装配与端到端测试

```go
// 文件：cluster_test.go
package kv

import "testing"

func TestRouterWriteQuorum(t *testing.T) {
	// 3 个分片，每分片 3 副本；为聚焦路由与提交，只装配各分片的 leader。
	var groups []Group
	for i := 0; i < 3; i++ {
		groups = append(groups, NewReplicaGroup(i, 3, true))
	}
	r := NewRouter(groups)
	defer r.Close()
	if _, err := r.Apply(Command{Op: OpSet, Key: "order:9", Value: []byte("paid")}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Apply(Command{Op: OpGet, Key: "order:9"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Value) != "paid" {
		t.Fatalf("got %q", got.Value)
	}
}
```

## 失败路径与边界条件

| 失败场景 | 后果 | 取舍 / 处理 |
| --- | --- | --- |
| 偶数副本发生网络分区 | 两边各半，都可能自认为可用 | 副本数取奇数，写必须超过半数 |
| 3 副本挂 2 个 | 无多数派，写全部失败 | 保一致性、牺牲可用性，符合 CP 取舍 |
| Leader 切换 | 未提交日志可能被覆盖 | 用任期与日志匹配回退，客户端需可重试且幂等 |
| 读打到 follower | 读到未提交的旧值 | 强一致读走 leader，或用 ReadIndex / lease read |
| 跨两个 key 的写 | 落在不同分片，无法原子提交 | 不提供跨分片事务，业务侧用 Saga（第 115 讲） |
| 分片扩容 | 取模路由导致几乎所有 key 迁移 | 换成一致性哈希 + 虚拟节点，只迁少量 key |
| 慢副本 | 拖长提交等待 | 多数派即可返回，不等最慢的那个 |

<Callout type="warn" title="跨分片没有事务，是本讲最贵的取舍" text="分片换来容量，代价是「单 key 原子、跨 key 不原子」。转账、下单这类必须跨 key 的操作，要么把相关 key 用同一分片键绑在一起，要么在业务层用 Saga 补偿——不能假装分布式事务不存在。这也是第 83 讲 CAP、第 85 讲分布式事务在真实系统里的落点。" />

## 深入一步：一致性到底从哪来

Raft 不追求「每个副本时时刻刻相同」，而是追求**所有副本按相同顺序应用相同的一串命令**。只要日志顺序一致、每条日志经多数派确认、且只有已提交的才应用到状态机，那么各副本最终状态必然收敛。这也是为什么 `Command` 从第一讲起就被设计成纯数据：它正是日志里要复制、要重放的那个东西。

<Compare title="分片键定位的两种方式" :panels='[
  {"label":"取模分片","code":"idx := hash(key) % N","note":"本讲 Router 的做法，简单均匀，但 N 变化时几乎全量迁移"},
  {"label":"一致性哈希","code":"ring.Get(key) // 带虚拟节点","note":"扩容只影响相邻一小段 key，是第 87 讲的进阶方案"}
]' />

<Term word="线性一致性" text="每个读都能看到在此之前已完成的写，表现得如同只有一份数据，是单 key 最强的一致性档位" />

`raftCore.apply` 被刻意放在锁外调用，因为状态机回调会取 `ReplicaGroup.mu`；若持着 `raftCore.mu` 去取另一把锁，而别的路径以相反顺序取锁，就会死锁。这类**锁顺序**问题在分布式组件里比单机更隐蔽，第 113 讲的快照也用了同一条规则。

## 测试与压测思路

- **提交语义**：构造 3 副本只给 1 个 ack 的用例，断言 `isCommitted` 为假并返回 `ErrNoQuorum`；再补足到 2 个 ack 断言提交成功。
- **幂等重放**：对同一条日志 `ack` 多次、或重复 `apply` 同一 `Command`，断言状态不变。
- **路由一致性**：对同一 key 连续 `SET`/`GET`，断言永远落在同一分片；用大量 key 统计各分片分布，观察倾斜。
- **故障注入**：让一个 follower 始终不响应，验证写仍能靠其余多数派完成；再让第二个下线，验证写被正确拒绝而不是悄悄成功。
- **压测**：`benchstat` 对比单机 `WALStorage` 与 `Router` 的写延迟，量化一次多数派确认的代价；用 Zipf 分布造热点 key，观察是否出现单分片过热。

## 工程实践 / 真实场景

生产里不会自己实现共识：`etcd/raft` 或 `hashicorp/raft` 提供选举、心跳、日志持久化、快照与成员变更，应用只负责把 `Command` 交给状态机。多个分片各自是一个独立 Raft 组（multi-Raft），热点分片还要能分裂与迁移；成员变更必须用联合共识（joint consensus）逐步过渡，直接替换成员列表会在过渡期出现两个互不相交的多数派，造成脑裂。

## 可继续扩展的方向

- 用 `etcd/raft` 替换 `raftCore`，接上真实网络、选举与日志持久化。
- 实现 ReadIndex 与 follower read，把读从 leader 上卸载。
- 用一致性哈希 + 虚拟节点替换取模路由，支持平滑扩容与分片分裂。
- 增加 Learner 节点、动态成员变更与多 Raft 组的统一调度。

## 动手练一练

<Exercise title="验证多数派故障容忍" task="基于 ReplicaGroup 写一个测试：3 副本时让一个 follower 不响应仍能写成功，让两个不响应则写必须失败。提示可通过控制 ack 的次数来模拟。" :hints='["把 Apply 里同步 ack 的次数做成可注入字段","3 副本的多数派是 2 票：自己 1 票加 1 个 follower 即可","失败路径要断言返回 ErrNoQuorum，且状态机没有该 key"]' solution="给 ReplicaGroup 增加 ackCount 字段（默认 peers-1）；Apply 循环改为 i 小于 ackCount 时调用 g.core.ack(idx)。测试一：peers=3、ackCount=1，写后断言 isCommitted 为真且 GET 命中；测试二：peers=3、ackCount=0，断言 Apply 返回 ErrNoQuorum 且 GET 返回 ErrNotFound。" explain="这道题把「多数派」从概念变成可验证的断言：理解 2 票即可提交、1 票必须拒绝，才算真正读懂了 Raft 的可用性边界。" />

## 随堂检测

<Quiz no="114-1" q="3 副本的 Raft 组，最多能容忍几个节点故障仍可写？" :options='["0 个","1 个","2 个","3 个"]' answer="1" explain="写需要超过半数即 2 票。挂 1 个还剩 2 个可形成多数派；挂 2 个只剩 1 票，为防脑裂必须拒绝写入。副本数取奇数正是为了让「多数」不产生歧义。" />

<Predict no="114-2" q="把请求发给 follower 做强一致读，最可能读到什么？" :options='["一定是最新的值","可能读到尚未提交的旧值","直接 panic","自动转发给 leader 并返回最新值"]' answer="1" output="可能读到尚未提交的旧值" explain="follower 的日志可能落后于 leader，直接读会破坏线性一致性。要么把读路由到 leader，要么用 ReadIndex 先确认 leader 身份与提交位点；示例里用 ErrNotLeader 明确拒绝。" />

<Quiz no="114-3" q="分片带来的最重要限制是什么？" :options='["读变慢了","无法保证单 key 原子","跨 key / 跨分片没有事务","必须用奇数分片"]' answer="2" explain="单 key 仍可原子且一致，但不同 key 可能落在不同分片的不同 Raft 组，无法用一个本地事务包住，需要业务层 Saga 或把相关 key 绑定到同一分片键。" />

## 一页纸速记

<KnowledgeCard :points='["Router 按 key 路由到唯一分片，自己仍实现 Storage，契约四讲未改","Raft 靠「同序日志 + 多数派提交」收敛，而不是同步写所有副本","奇数副本 + 超过半数确认，是防脑裂与容忍少数故障的关键","强一致读必须走 leader 或 ReadIndex，follower 可能读到旧值","单 key 原子、跨 key 无事务，是分片最贵的取舍"]' :pitfalls='["用偶数副本，分区时两边各半都可能自认为可用","把读随路由到 follower 还以为拿到最新值","跨分片假装存在分布式事务","apply 回调持着 raftCore 锁再取状态机锁，埋下死锁"]' summary="分片解决容量、副本解决单点、Raft 解决顺序一致，代价是跨分片事务与多数派可用性边界。" />

## 小结与下一讲

四讲连起来看，是同一套 `Command` 与 `Storage` 背后换了四次实现：**内存**（定契约）→ **网络**（Client 也实现接口）→ **WAL**（落盘与锁分片）→ **分布式**（分片、副本、Raft）。每一版都由上一版的具体痛点逼出来，而调用方的 `Apply` 从头到尾没变过——这就是接口作为「窄腰」的价值。

这一版明确放弃了跨分片事务，因为真实业务里「下单」偏偏要跨订单、库存、支付三处数据。下一讲离开 KV 贯穿线，进入电商微服务系统，正面处理服务拆分与分布式事务。

[下一讲：实战 09 · 电商微服务系统](/projects/micro-ecommerce)
