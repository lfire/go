package main

import (
	"fmt"
	"sync"
	"time"
)

// TokenBucket 令牌桶：按固定速率补充令牌，拿到令牌才放行，允许一定突发。
type TokenBucket struct {
	mu       sync.Mutex
	capacity float64
	rate     float64 // 每秒补充的令牌数
	tokens   float64
	last     time.Time
}

func NewTokenBucket(capacity, rate float64) *TokenBucket {
	return &TokenBucket{capacity: capacity, rate: rate, tokens: capacity, last: time.Now()}
}

func (b *TokenBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens += elapsed * b.rate
	if b.tokens > b.capacity {
		b.tokens = b.capacity // 桶满则截断，避免无限累积
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func main() {
	b := NewTokenBucket(3, 2) // 容量 3，每秒补充 2 个
	for i := 0; i < 5; i++ {
		fmt.Printf("第 %d 次请求 放行=%v\n", i+1, b.Allow())
	}
	fmt.Println("等待令牌补充...")
	time.Sleep(1100 * time.Millisecond)
	fmt.Println("补充后 放行=", b.Allow())
}
