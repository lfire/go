package main

import (
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"
)

// QueryMetrics 用原子计数统计查询，避免高频路径锁竞争。
type QueryMetrics struct {
	queries atomic.Int64
	rows    atomic.Int64
	errors  atomic.Int64
}

// Observe 在查询结束时统一记录耗时、行数与错误，供 EXPLAIN 之外定位慢查询。
func Observe(logger *slog.Logger, m *QueryMetrics, sqlText string, rows int, started time.Time, err error) {
	m.queries.Add(1)
	m.rows.Add(int64(rows))
	if err != nil {
		m.errors.Add(1)
	}
	logger.Info("query_done",
		slog.String("sql", sqlText),
		slog.Int("rows", rows),
		slog.Duration("cost", time.Since(started)),
		slog.Bool("has_error", err != nil),
	)
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	m := &QueryMetrics{}
	Observe(logger, m, "SELECT status FROM access", 2, time.Now().Add(-3*time.Millisecond), nil)
	fmt.Printf("queries=%d rows=%d errors=%d\n", m.queries.Load(), m.rows.Load(), m.errors.Load())
}
