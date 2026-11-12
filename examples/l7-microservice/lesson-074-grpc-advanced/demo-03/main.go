package main

import (
	"context"
	"fmt"
	"time"
)

// grpcTimeout 模拟 gRPC：把 ctx 剩余时间编码成 grpc-timeout metadata 传给下游。
func grpcTimeout(ctx context.Context) string {
	d, ok := ctx.Deadline()
	if !ok {
		return "" // 没有 deadline 就不发这个 header
	}
	ms := time.Until(d).Milliseconds()
	if ms < 0 {
		ms = 0
	}
	return fmt.Sprintf("%dm", ms) // 300m 表示 300 毫秒
}

// downstream 尊重上游 deadline，到点立即收手，不傻等到自己超时。
func downstream(ctx context.Context) error {
	select {
	case <-time.After(2 * time.Second):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func main() {
	// 顶层预算 500 毫秒，沿链路用同一个 ctx 派生
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	fmt.Println("发给下游的 grpc-timeout:", grpcTimeout(ctx))

	child, cancel2 := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel2()
	fmt.Println("子调用 grpc-timeout:", grpcTimeout(child))
	fmt.Println("下游结果:", downstream(child))
}
