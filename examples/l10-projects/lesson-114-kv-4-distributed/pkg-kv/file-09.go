// 文件：metrics.go
package kv

import "sync/atomic"

// ClusterMetrics 用原子计数埋点，避免可观测性路径成为 Raft 组的锁竞争点。
// 字段与第 9 节指标表一一对应，Read 导出一份供 /metrics 抓取的快照。
type ClusterMetrics struct {
	routed           atomic.Int64
	writes           atomic.Int64
	reads            atomic.Int64
	proposals        atomic.Int64
	commits          atomic.Int64
	leaderChanges    atomic.Int64
	quorumFailures   atomic.Int64
	splitBrainAlerts atomic.Int64
	migrations       atomic.Int64
	staleReads       atomic.Int64
}

func (m *ClusterMetrics) Read() map[string]int64 {
	return map[string]int64{
		"routed_total":       m.routed.Load(),
		"writes_total":       m.writes.Load(),
		"reads_total":        m.reads.Load(),
		"proposals_total":    m.proposals.Load(),
		"commits_total":      m.commits.Load(),
		"leader_changes":     m.leaderChanges.Load(),
		"quorum_failures":    m.quorumFailures.Load(),
		"split_brain_alerts": m.splitBrainAlerts.Load(),
		"migrations":         m.migrations.Load(),
		"stale_reads":        m.staleReads.Load(),
	}
}
