package main

import (
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
)

// Metrics 用原子计数器承载调度核心指标。
type Metrics struct {
	Fired      atomic.Int64
	Succeeded  atomic.Int64
	Failed     atomic.Int64
	TimedOut   atomic.Int64
	LeaderSwap atomic.Int64
	Fenced     atomic.Int64
}

func (m *Metrics) Snapshot() string {
	return fmt.Sprintf("fired=%d ok=%d fail=%d timeout=%d swap=%d fenced=%d",
		m.Fired.Load(), m.Succeeded.Load(), m.Failed.Load(),
		m.TimedOut.Load(), m.LeaderSwap.Load(), m.Fenced.Load())
}

var logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))

func record(jobID string, epoch int64, state string, costMs int64, m *Metrics) {
	switch state {
	case "fire":
		m.Fired.Add(1)
	case "ok":
		m.Succeeded.Add(1)
	case "timeout":
		m.TimedOut.Add(1)
	case "fenced":
		m.Fenced.Add(1)
	default:
		m.Failed.Add(1)
	}
	logger.Info("job_event",
		"job_id", jobID,
		"epoch", epoch,
		"state", state,
		"cost_ms", costMs,
	)
}

func recordSwap(m *Metrics) {
	m.LeaderSwap.Add(1)
	logger.Warn("leader_swap", "event", "failover")
}

func main() {
	m := &Metrics{}
	record("report-daily", 3, "fire", 0, m)
	record("report-daily", 3, "ok", 137, m)
	record("cleanup", 3, "timeout", 5000, m)
	record("cleanup", 2, "fenced", 0, m)
	recordSwap(m)
	fmt.Println(m.Snapshot())
}
