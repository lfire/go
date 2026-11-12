package kv

import (
	"sync/atomic"
	"time"
)

// NetMetrics 记录服务端的连接、帧、错误与超时，用 atomic 避免自身成为锁瓶颈。
type NetMetrics struct {
	conns    atomic.Int64
	frames   atomic.Uint64
	errors   atomic.Uint64
	timeouts atomic.Uint64
	pings    atomic.Uint64
	totalNs  atomic.Int64
}

func (m *NetMetrics) incConn() { m.conns.Add(1) }
func (m *NetMetrics) decConn() { m.conns.Add(-1) }

// Record 统计一帧的处理结果与耗时；超时由调用方按 net.Error 另记。
func (m *NetMetrics) Record(err error, d time.Duration) {
	m.frames.Add(1)
	m.totalNs.Add(d.Nanoseconds())
	if err != nil {
		m.errors.Add(1)
	}
}

// Snapshot 返回活动连接数、累计帧数、错误数、心跳数与平均耗时微秒。
func (m *NetMetrics) Snapshot() (conns int64, frames, errs, pings uint64, avgUs float64) {
	conns = m.conns.Load()
	frames = m.frames.Load()
	errs = m.errors.Load()
	pings = m.pings.Load()
	if frames > 0 {
		avgUs = float64(m.totalNs.Load()) / float64(frames) / 1000
	}
	return
}
