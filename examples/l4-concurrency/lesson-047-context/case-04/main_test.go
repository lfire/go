package main

import (
	"context"
	"testing"
	"time"
)

// 反例：ctx, _ := context.WithCancel(parent) 丢掉 cancel，
// go vet ./... 会报 "the cancel function ... should be called, not discarded"
// 错误对照：丢弃上游 ctx，另起炉灶，取消信号传不进来
func slowNoCtx() string {
	time.Sleep(200 * time.Millisecond)
	return "完成"
}

// 正确：透传 ctx，select 监听 Done，取消时立刻收手
func slowWithCtx(ctx context.Context) (string, error) {
	select {
	case <-time.After(200 * time.Millisecond):
		return "完成", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := slowWithCtx(ctx)
	if err != context.DeadlineExceeded {
		t.Fatalf("期望超时，实际 %v", err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("取消未及时生效，耗时 %v", d)
	}
	_ = slowNoCtx // 对照：它无视取消，必然跑满 200ms
}
