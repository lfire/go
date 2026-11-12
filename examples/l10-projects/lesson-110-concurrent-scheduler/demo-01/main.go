package main

import (
	"context"
	"time"
)

// CronExpr 把 cron 表达式抽象成「给定时刻求下次触发」的能力。
type CronExpr interface {
	Next(after time.Time) time.Time
}

// State 是任务状态机的状态枚举。
type State int32

// Transition 校验状态转换是否合法。
type Transition interface {
	Can(from, to State) bool
}

// Executor 在隔离环境执行任务，必须尊重 ctx 超时与取消。
type Executor interface {
	Run(ctx context.Context, j Job) (Result, error)
}

// Election 抽象选主：抢主、退位、失主信号、带 epoch 的派发资格。
type Election interface {
	Campaign(ctx context.Context, node string) (int64, error)
	Resign(ctx context.Context) error
	Done() <-chan struct{}
	IsLeader(epoch int64) bool
}

// Sharder 按分片键把任务映射到负责节点。
type Sharder interface {
	Owner(key string) string
}

// Job 与 Result 是跨层帧，字段可序列化且有零值语义。
type Job struct {
	ID       string
	Cron     string
	Timeout  time.Duration
	Retries  int
	ShardKey string
}

type Result struct {
	JobID  string
	Epoch  int64
	OK     bool
	Log    string
	CostMs int64
}

func main() {}
