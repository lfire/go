// 文件：membership.go
package kv

import "sync"

// MigrationState 描述一次成员变更的过渡阶段。
// 直接替换成员列表会在过渡期出现两个互不相交的多数派，必须用联合共识逐步过渡。
type MigrationState uint8

const (
	MigrationIdle  MigrationState = iota
	MigrationJoint                // 新旧配置同时生效，迁移区间双写
	MigrationDone                 // 数据校验一致，等待切换路由
)

type Membership struct {
	mu      sync.RWMutex
	ring    *HashRing
	groups  map[string]Group
	state   MigrationState
	pending []Migration
}

func NewMembership(replicasPerNode int) *Membership {
	return &Membership{
		ring:   NewHashRing(replicasPerNode),
		groups: make(map[string]Group),
		state:  MigrationIdle,
	}
}

// AddNode 触发一次成员变更：加入节点后若产生迁移区间，就进入 Joint 阶段。
// 首个节点是冷启动建集群，不算扩容，不产生迁移任务。
func (m *Membership) AddNode(node string, g Group) []Migration {
	m.mu.Lock()
	defer m.mu.Unlock()
	hadNodes := len(m.groups) > 0
	migs := m.ring.Add(node)
	m.groups[node] = g
	if hadNodes && len(migs) > 0 {
		m.state = MigrationJoint
		m.pending = migs
	}
	return migs
}

// RemoveNode 摘除节点，返回区间接管任务；不立即删组，避免 Joint 期间读不到源数据。
func (m *Membership) RemoveNode(node string) []Migration {
	m.mu.Lock()
	defer m.mu.Unlock()
	migs := m.ring.Remove(node)
	if len(migs) > 0 {
		m.state = MigrationJoint
		m.pending = migs
	}
	return migs
}

func (m *Membership) State() MigrationState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Membership) Pending() []Migration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Migration(nil), m.pending...)
}

// FinishMigration 在源与目标数据校验一致后推进到 Done，
// 这是唯一允许切换路由的时机，避免过渡期出现双主。
func (m *Membership) FinishMigration() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = MigrationDone
	m.pending = nil
}

// Reset 在运维确认新配置稳定后回到 Idle，成员变更闭环。
func (m *Membership) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = MigrationIdle
	m.pending = nil
}

// Route 按 key 返回目标组；Joint 阶段区间内的 key 仍由源组服务，双写由 Router 处理。
func (m *Membership) Route(key string) (Group, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	node := m.ring.Get(key)
	if node == "" {
		return nil, ErrNoQuorum
	}
	g := m.groups[node]
	if g == nil {
		return nil, ErrNotLeader
	}
	return g, nil
}

func (m *Membership) Groups() map[string]Group {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]Group, len(m.groups))
	for k, v := range m.groups {
		out[k] = v
	}
	return out
}

func (m *Membership) Ring() *HashRing { return m.ring }
