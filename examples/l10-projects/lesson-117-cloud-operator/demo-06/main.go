// 文件：internal/metrics/metrics.go
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics 对应 Prometheus 的 Counter 与 Histogram，全部用原子或短锁保护。
type Metrics struct {
	Reconciles atomic.Int64
	Failures   atomic.Int64
	DurationUS atomic.Int64
	QueueDepth atomic.Int64
	mu         sync.Mutex
	failReason map[string]int64
}

func NewMetrics() *Metrics {
	return &Metrics{failReason: make(map[string]int64)}
}

func (m *Metrics) incFailure(reason string) {
	m.Failures.Add(1)
	m.mu.Lock()
	m.failReason[reason]++
	m.mu.Unlock()
}

func (m *Metrics) FailByReason(reason string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.failReason[reason]
}

// Observe 在每次调谐收尾统一埋点：计数、耗时、结构化日志三件事一次做完。
func Observe(m *Metrics, logger *slog.Logger, cr string, gen int64, action string, start time.Time, err error) {
	m.Reconciles.Add(1)
	m.DurationUS.Add(time.Since(start).Microseconds())
	if err != nil {
		m.incFailure(action)
		logger.Error("reconcile_failed",
			"cr", cr, "generation", gen, "action", action,
			"duration_ms", time.Since(start).Milliseconds(), "err", err)
		return
	}
	logger.Info("reconcile_ok",
		"cr", cr, "generation", gen, "action", action,
		"duration_ms", time.Since(start).Milliseconds())
}

// ErrorBudgetBurn 返回失败占比；总量为零时定义为 0，避免除零与误告警。
func ErrorBudgetBurn(failures, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return failures / total
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	m := NewMetrics()
	start := time.Now()
	Observe(m, logger, "kv-prod", 7, "rollout", start, nil)
	Observe(m, logger, "kv-prod", 7, "status", start, errors.New("apiserver 409 conflict"))
	fmt.Println("reconciles:", m.Reconciles.Load())
	fmt.Println("status failures:", m.FailByReason("status"))
	fmt.Printf("burn rate: %.3f\n", ErrorBudgetBurn(float64(m.Failures.Load()), float64(m.Reconciles.Load())))
}
