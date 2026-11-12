package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
)

type Metrics struct {
	Redirect atomic.Int64
	Hit      atomic.Int64
	Miss     atomic.Int64
	Timeout  atomic.Int64
}

func (m *Metrics) Observe(hit bool, costMs int64, status int) {
	m.Redirect.Add(1)
	if hit {
		m.Hit.Add(1)
	} else {
		m.Miss.Add(1)
	}
	if status >= 500 {
		m.Timeout.Add(1)
	}
	slog.Info("redirect", "cost_ms", costMs, "status", status, "hit", hit)
}

func handlerTrace(ctx context.Context, code string) context.Context {
	tid, ok := ctx.Value("trace_id").(string)
	if !ok || tid == "" {
		tid = fmt.Sprintf("gen-%d", os.Getpid())
	}
	return context.WithValue(ctx, "trace_id", tid)
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	m := &Metrics{}
	ctx := handlerTrace(context.WithValue(context.Background(), "trace_id", "t-abc123"), "7ZkQ2f")
	tid, _ := ctx.Value("trace_id").(string)
	for _, hit := range []bool{true, true, false} {
		status := 302
		if !hit {
			status = 404
		}
		m.Observe(hit, 1, status)
	}
	fmt.Println("trace_id=", tid, "redirect=", m.Redirect.Load(),
		"hit=", m.Hit.Load(), "miss=", m.Miss.Load())
}
