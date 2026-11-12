// 文件：metrics.go
package kv

import "sync/atomic"

// Metrics 用原子计数埋点，避免可观测性路径反过来成为锁竞争点。
// 字段与第 9 节的指标表一一对应，Read 导出一份供 /metrics 抓取的快照。
type Metrics struct {
	writes        atomic.Int64
	reads         atomic.Int64
	walBytes      atomic.Int64
	syncCount     atomic.Int64
	replayRecords atomic.Int64
	compactions   atomic.Int64
	partialWrites atomic.Int64
	diskFull      atomic.Int64
	crcFailures   atomic.Int64
}

func (m *Metrics) Read() map[string]int64 {
	return map[string]int64{
		"writes_total":    m.writes.Load(),
		"reads_total":     m.reads.Load(),
		"wal_bytes_total": m.walBytes.Load(),
		"fsync_total":     m.syncCount.Load(),
		"replay_records":  m.replayRecords.Load(),
		"compactions":     m.compactions.Load(),
		"partial_writes":  m.partialWrites.Load(),
		"disk_full_total": m.diskFull.Load(),
		"crc_failures":    m.crcFailures.Load(),
	}
}
