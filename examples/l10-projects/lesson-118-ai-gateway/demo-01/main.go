// 文件：api/contract.go
package main

import (
	"context"
	"fmt"
)

// Chunk 是网关对外的统一流式片段，屏蔽各供应商字段差异。
type Chunk struct {
	Text  string
	Done  bool
	Usage int
}

// ChatRequest 是网关统一入参。
type ChatRequest struct {
	Model    string
	Messages []string
}

// Provider 把「一次对话」抽象成统一块流，emit 每推一块就向下游转一块。
type Provider interface {
	Name() string
	Stream(ctx context.Context, req ChatRequest, emit func(Chunk) error) error
}

// Limiter 是限流边界，按 API Key 判额度与优先级。
type Limiter interface {
	Allow(key string) bool
}

// Meter 是成本与延迟的出口，实现方决定落到 Prometheus 还是账单系统。
type Meter interface {
	AddTokens(key, provider string, n int)
	ObserveLatency(provider string, d int64)
}

// stubProvider 用于演示接口可被最小实现满足，编译期校验方法签名齐全。
type stubProvider struct{}

func (stubProvider) Name() string { return "stub" }
func (stubProvider) Stream(_ context.Context, _ ChatRequest, emit func(Chunk) error) error {
	return emit(Chunk{Text: "ok", Done: true})
}

func main() {
	var p Provider = stubProvider{}
	err := p.Stream(context.Background(), ChatRequest{Model: "m"}, func(c Chunk) error {
		fmt.Println("chunk:", c.Text, "done:", c.Done)
		return nil
	})
	fmt.Println("stream ok:", err == nil)
}
