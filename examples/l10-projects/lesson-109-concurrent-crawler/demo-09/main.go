package main

import (
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
)

// Metrics 用原子计数器承载核心指标，避免高频路径加锁。
type Metrics struct {
	Fetched atomic.Int64
	Deduped atomic.Int64
	Failed  atomic.Int64
	Retried atomic.Int64
	BytesIn atomic.Int64
}

func (m *Metrics) Snapshot() string {
	return fmt.Sprintf("fetched=%d deduped=%d failed=%d retried=%d bytes=%d",
		m.Fetched.Load(), m.Deduped.Load(), m.Failed.Load(), m.Retried.Load(), m.BytesIn.Load())
}

var logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))

func recordFetch(traceID, url string, status int, costMs int64, m *Metrics) {
	m.Fetched.Add(1)
	m.BytesIn.Add(4096)
	logger.Info("fetch",
		"trace_id", traceID,
		"url", url,
		"status", status,
		"cost_ms", costMs,
		"event", "page_done",
	)
}

func recordFailure(traceID, url, reason string, m *Metrics) {
	m.Failed.Add(1)
	logger.Warn("fetch_failed",
		"trace_id", traceID,
		"url", url,
		"reason", reason,
		"event", "page_fail",
	)
}

func main() {
	m := &Metrics{}
	recordFetch("t-001", "/item/1", 200, 137, m)
	recordFetch("t-001", "/item/2", 200, 210, m)
	recordFailure("t-001", "/item/3", "timeout", m)
	fmt.Println(m.Snapshot())
}
