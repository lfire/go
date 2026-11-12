// 文件：metrics.go —— 秒杀水位与限流埋点（快照一致读取）
package main

import (
	"fmt"
	"sync"
)

// Metrics 一次 Snapshot 取全量，避免大盘各指标来自不同时刻。
type Metrics struct {
	mu       sync.Mutex
	accepted int64
	rejected int64
	soldOut  int64
	left     int
}

func (m *Metrics) IncAccepted()  { m.mu.Lock(); defer m.mu.Unlock(); m.accepted++ }
func (m *Metrics) IncRejected()  { m.mu.Lock(); defer m.mu.Unlock(); m.rejected++ }
func (m *Metrics) IncSoldOut()   { m.mu.Lock(); defer m.mu.Unlock(); m.soldOut++ }
func (m *Metrics) SetLeft(n int) { m.mu.Lock(); defer m.mu.Unlock(); m.left = n }

func (m *Metrics) Snapshot() (accepted, rejected, soldOut int64, left int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.accepted, m.rejected, m.soldOut, m.left
}

func main() {
	m := &Metrics{}
	for i := 0; i < 1000; i++ {
		m.IncRejected()
	}
	for i := 0; i < 100; i++ {
		m.IncAccepted()
	}
	m.IncSoldOut()
	m.SetLeft(0)
	a, r, s, left := m.Snapshot()
	fmt.Printf("accepted=%d rejected=%d soldOut=%d left=%d\n", a, r, s, left)
}
