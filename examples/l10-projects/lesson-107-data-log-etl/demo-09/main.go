package main

import (
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
)

// Metrics 用原子计数避免高频路径上的锁竞争，供 Prometheus 拉取。
type Metrics struct {
	lines   atomic.Int64
	bad     atomic.Int64
	late    atomic.Int64
	flushes atomic.Int64
}

func (m *Metrics) Snapshot() string {
	return fmt.Sprintf("lines=%d bad=%d late=%d flushes=%d",
		m.lines.Load(), m.bad.Load(), m.late.Load(), m.flushes.Load())
}

// logBad 用结构化日志记录坏行，来源与位点让一次格式变更可还原。
func logBad(logger *slog.Logger, source string, offset int64, reason string) {
	logger.Warn("坏行进入死信",
		slog.String("source", source),
		slog.Int64("offset", offset),
		slog.String("reason", reason),
		slog.String("trace_id", "etl-107-demo"),
	)
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	m := &Metrics{}
	m.lines.Add(3)
	m.bad.Add(1)
	m.late.Add(1)
	m.flushes.Add(2)
	logBad(logger, "app.log", 42, "字段数不为四")
	fmt.Println(m.Snapshot())
}
