// 文件：internal/observability/metrics.go
package main

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Latency 记录一个供应商的 TTFT 样本，用于算分位数。
type Latency struct {
	mu      sync.Mutex
	samples []float64
}

func (l *Latency) Add(ms float64) {
	l.mu.Lock()
	l.samples = append(l.samples, ms)
	l.mu.Unlock()
}

func (l *Latency) P99() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.samples) == 0 {
		return 0
	}
	cp := append([]float64(nil), l.samples...)
	sort.Float64s(cp)
	idx := int(float64(len(cp)-1) * 0.99)
	return cp[idx]
}

// Cost 用原子计数累计 token，避免热路径锁竞争。
type Cost struct {
	tokens atomic.Int64
}

func (c *Cost) Add(n int)     { c.tokens.Add(int64(n)) }
func (c *Cost) Tokens() int64 { return c.tokens.Load() }

// EstimateCost 按每百万 token 的单价（单位：分）换算，参数显式可配。
func EstimateCost(completionTokens int64, pricePerMillion float64) float64 {
	return float64(completionTokens) / 1e6 * pricePerMillion
}

// Metrics 聚合一次流的延迟、成本与结构化日志。
type Metrics struct {
	TTFT   *Latency
	Cost   *Cost
	logger *slog.Logger
}

func NewMetrics() *Metrics {
	return &Metrics{
		TTFT:   &Latency{},
		Cost:   &Cost{},
		logger: slog.New(slog.NewJSONHandler(os.Stdout, nil)),
	}
}

// ObserveStream 在流结束后统一结算，不占转发关键路径。
func (m *Metrics) ObserveStream(traceID, provider, model string, ttft time.Duration, completionTokens int, err error) {
	m.TTFT.Add(float64(ttft.Milliseconds()))
	m.Cost.Add(completionTokens)
	m.logger.Info("stream_done",
		"trace_id", traceID,
		"provider", provider,
		"model", model,
		"ttft_ms", ttft.Milliseconds(),
		"completion_tokens", completionTokens,
		"estimated_cent", EstimateCost(int64(completionTokens), 200),
		"err", err)
}

func main() {
	m := NewMetrics()
	m.ObserveStream("t-001", "openai", "gpt-4o-mini", 320*time.Millisecond, 128, nil)
	m.ObserveStream("t-002", "anthropic", "claude", 410*time.Millisecond, 256, nil)
	fmt.Printf("ttft p99: %.0f ms\n", m.TTFT.P99())
	fmt.Printf("total tokens: %d cost: %.4f cent\n", m.Cost.Tokens(), EstimateCost(m.Cost.Tokens(), 200))
}
