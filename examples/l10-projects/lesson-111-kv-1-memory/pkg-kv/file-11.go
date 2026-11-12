package kv

import (
	"sync/atomic"
	"time"
)

var _ Storage = (*MeteredStorage)(nil)

// MeteredStorage 在不改动任何实现的前提下为任意 Storage 加埋点。
// 计数器用 atomic；耗时此处累计总和与次数，生产可换成 Histogram。
type MeteredStorage struct {
	inner   Storage
	ops     [4]atomic.Uint64 // 按 Op 数值下标计数
	errs    atomic.Uint64
	totalNs atomic.Int64
	calls   atomic.Uint64
}

func NewMeteredStorage(inner Storage) *MeteredStorage {
	return &MeteredStorage{inner: inner}
}

func (m *MeteredStorage) Apply(cmd Command) (Result, error) {
	start := time.Now()
	r, err := m.inner.Apply(cmd)
	elapsed := time.Since(start).Nanoseconds()
	m.calls.Add(1)
	m.totalNs.Add(elapsed)
	if int(cmd.Op) < len(m.ops) {
		m.ops[cmd.Op].Add(1)
	}
	if err != nil {
		m.errs.Add(1)
	}
	return r, err
}

func (m *MeteredStorage) Close() error { return m.inner.Close() }

// Snapshot 返回累计调用数、错误数与平均耗时（微秒），供 CLI 与监控采集。
func (m *MeteredStorage) Snapshot() (calls, errs uint64, avgUs float64) {
	c := m.calls.Load()
	calls = c
	errs = m.errs.Load()
	if c > 0 {
		avgUs = float64(m.totalNs.Load()) / float64(c) / 1000
	}
	return
}
