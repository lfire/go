package main

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

// Counter 只增不减，用于累计请求数、错误数。
type Counter struct{ v atomic.Uint64 }

func (c *Counter) Inc()            { c.v.Add(1) }
func (c *Counter) Value() uint64   { return c.v.Load() }

// Gauge 可增可减，表示瞬时值，如当前在途请求数、队列长度。
type Gauge struct{ v atomic.Int64 }

func (g *Gauge) Inc()         { g.v.Add(1) }
func (g *Gauge) Dec()         { g.v.Add(-1) }
func (g *Gauge) Value() int64 { return g.v.Load() }

// Histogram 把观测值分桶累计，同时记录总和与次数。
type Histogram struct {
	mu      sync.Mutex
	bounds  []float64
	buckets []uint64
	sum     float64
	count   uint64
}

func NewHistogram(bounds []float64) *Histogram {
	b := append([]float64(nil), bounds...)
	sort.Float64s(b)
	return &Histogram{bounds: b, buckets: make([]uint64, len(b))}
}

func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sum += v
	h.count++
	for i, upper := range h.bounds {
		if v <= upper { // 累计桶：记录小于等于该上界的总次数
			h.buckets[i]++
		}
	}
}

func (h *Histogram) Snapshot() ([]uint64, float64, uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]uint64(nil), h.buckets...), h.sum, h.count
}

func main() {
	var req Counter
	var inflight Gauge
	lat := NewHistogram([]float64{10, 50, 100, 200, 500})

	for _, ms := range []float64{5, 12, 60, 130, 400} {
		req.Inc()
		inflight.Inc()
		lat.Observe(ms)
		inflight.Dec()
	}
	buckets, sum, count := lat.Snapshot()
	fmt.Println("累计请求:", req.Value(), "在途:", inflight.Value())
	fmt.Println("分桶累计:", buckets, "总和:", sum, "次数:", count)
}
