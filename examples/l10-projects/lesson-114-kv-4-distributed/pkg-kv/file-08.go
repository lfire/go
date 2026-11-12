// 文件：router.go
package kv

import "sync"

var _ Storage = (*Router)(nil)

// Router 按一致性哈希把命令路由到唯一分片，自己仍实现 Storage。
// 第 111 讲定型的契约在分布式入口处依旧成立，调用方从单机切到集群零改动。
type Router struct {
	membership *Membership
	metrics    *ClusterMetrics
	mu         sync.RWMutex
	closed     bool
}

func NewRouter(m *Membership) *Router {
	return &Router{membership: m, metrics: &ClusterMetrics{}}
}

func (r *Router) Apply(cmd Command) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return Result{}, ErrClosed
	}
	// 埋点放在路由层：它是所有请求的唯一入口，一次计数即可覆盖全链路。
	r.metrics.routed.Add(1)
	if cmd.Op == OpGet {
		r.metrics.reads.Add(1)
	} else {
		r.metrics.writes.Add(1)
	}
	g, err := r.membership.Route(cmd.Key)
	if err != nil {
		return Result{}, err
	}
	return g.Apply(cmd)
}

// CheckSameShard 判断一组 key 是否落在同一分片。
// 跨分片返回 ErrCrossShardTxn：本系统不提供跨分片事务，
// 业务层需把相关 key 绑定同一分片键，或改用 Saga 补偿（第 115 讲）。
func (r *Router) CheckSameShard(keys []string) (string, error) {
	if len(keys) == 0 {
		return "", nil
	}
	ring := r.membership.Ring()
	first := ring.Get(keys[0])
	for _, k := range keys[1:] {
		if ring.Get(k) != first {
			return "", ErrCrossShardTxn
		}
	}
	return first, nil
}

func (r *Router) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	var first error
	for _, g := range r.membership.Groups() {
		if err := g.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Metrics 暴露只读指标视图，供第 9 节的 /metrics 端点抓取。
func (r *Router) Metrics() map[string]int64 { return r.metrics.Read() }
