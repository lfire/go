// 文件：replica.go
package kv

import (
	"sync"
	"sync/atomic"
)

// Group 抽象一个分片对应的副本组；真实实现里它就是一个独立 Raft 组。
// 用接口隔离，路由层不依赖任何具体共识库。
type Group interface {
	Storage
	ShardID() int
}

var _ Group = (*ReplicaGroup)(nil)

const maxStaleness = 3

// ReplicaGroup 是一个分片的多副本组：只有 Leader 接受写，
// 写先经 raftCore 复制到多数派，提交后才落到本地状态机。
type ReplicaGroup struct {
	id        int
	peers     int
	core      *raftCore
	mu        sync.RWMutex
	data      map[string][]byte
	ackBudget atomic.Int64 // 本次写允许收到的 Follower 确认数，故障注入用
	reachable atomic.Int64 // 当前可见副本数，脑裂检查用
}

func NewReplicaGroup(id, peers int) *ReplicaGroup {
	if peers%2 == 0 {
		peers++ // 副本数取奇数，分区时不会出现两边各半
	}
	g := &ReplicaGroup{
		id: id, peers: peers,
		data: make(map[string][]byte),
	}
	g.ackBudget.Store(int64(peers - 1))
	g.reachable.Store(int64(peers))
	g.core = newRaftCore(0, peers)
	g.core.startElection()
	for i := 1; i < WriteQuorum(peers); i++ {
		g.core.recvVote()
	}
	return g
}

func (g *ReplicaGroup) ShardID() int { return g.id }

// SetAckBudget / SetReachable 用原子写，方便并发故障注入且不引入数据竞争。
func (g *ReplicaGroup) SetAckBudget(n int) { g.ackBudget.Store(int64(n)) }
func (g *ReplicaGroup) SetReachable(n int) { g.reachable.Store(int64(n)) }

// applyOne 把已提交命令应用到本地状态机，幂等，重放安全。
func (g *ReplicaGroup) applyOne(cmd Command) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch cmd.Op {
	case OpSet:
		g.data[cmd.Key] = append([]byte(nil), cmd.Value...)
	case OpDel:
		delete(g.data, cmd.Key)
	}
}

// Replicate 模拟 Leader 收到一个 Follower 的 AppendEntries 响应。
func (g *ReplicaGroup) Replicate(peer, index int) {
	for _, c := range g.core.ack(peer, index) {
		g.applyOne(c) // 在 raftCore 的锁之外应用，避免锁顺序反转
	}
}

// Read 暴露可选一致性级别的读入口；Storage.Apply 的 GET 默认走线性一致。
func (g *ReplicaGroup) Read(cmd Command, level Consistency) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	if cmd.Op != OpGet {
		return Result{}, ErrBadOp
	}
	// 强一致读必须走 Leader 或 ReadIndex；Follower 日志可能落后于提交位点。
	if !LevelAllowsFollowerRead(level) && !g.core.isLeader() {
		return Result{}, ErrNotLeader
	}
	if level == ConsistencyBoundedStaleness && g.core.lag() > maxStaleness {
		return Result{}, ErrStaleRead
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	v, ok := g.data[cmd.Key]
	if !ok {
		return Result{}, ErrNotFound
	}
	return Result{Value: append([]byte(nil), v...), Found: true}, nil
}

func (g *ReplicaGroup) Apply(cmd Command) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	if cmd.Op == OpGet {
		return g.Read(cmd, ConsistencyLinearizable)
	}
	if cmd.Op != OpSet && cmd.Op != OpDel {
		return Result{}, ErrBadOp
	}
	// 写前脑裂检查：可见副本不足多数派就拒绝提交，避免少数派分区独立推进。
	if err := CheckQuorum(g.peers, int(g.reachable.Load())); err != nil {
		return Result{}, err
	}
	idx, ok := g.core.propose(cmd)
	if !ok {
		return Result{}, ErrNotLeader
	}
	budget := int(g.ackBudget.Load())
	if budget > g.peers-1 {
		budget = g.peers - 1
	}
	for peer := 1; peer <= budget; peer++ {
		for _, c := range g.core.ack(peer, idx) {
			g.applyOne(c)
		}
	}
	if !g.core.committed(idx) {
		return Result{}, ErrNoQuorum
	}
	return Result{Found: true}, nil
}

func (g *ReplicaGroup) Close() error { return nil }
