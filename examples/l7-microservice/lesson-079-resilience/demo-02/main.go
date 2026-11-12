package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// RetryBudget 把重试量限制为总请求的一个比例，防止下游变慢时重试自我放大。
type RetryBudget struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	ratio    float64 // 每来一个正常请求补充 ratio 个重试额度
}

func NewRetryBudget(capacity, ratio float64) *RetryBudget {
	return &RetryBudget{tokens: 0, capacity: capacity, ratio: ratio}
}

func (b *RetryBudget) OnRequest() { // 每个原始请求补充一点额度
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens += b.ratio
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
}

func (b *RetryBudget) Take() bool { // 想重试先取一个额度
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// backoff 指数退避加随机抖动，避免大量客户端在同一瞬间齐步重试。
func backoff(attempt int, base time.Duration) time.Duration {
	d := base << attempt
	jitter := time.Duration(rand.Int63n(int64(d / 2)))
	return d + jitter
}

func main() {
	budget := NewRetryBudget(3, 0.2) // 最多攒 3 个额度，每请求补 0.2
	allowed, denied := 0, 0
	for i := 0; i < 20; i++ {
		budget.OnRequest()
		if budget.Take() {
			allowed++
		} else {
			denied++
		}
	}
	fmt.Printf("20 次请求: 允许重试 %d 次, 预算拒绝 %d 次\n", allowed, denied)
	_ = backoff // 真实调用里按 attempt 递增使用指数退避
}
