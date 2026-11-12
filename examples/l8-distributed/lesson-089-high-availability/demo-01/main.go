package main

import (
	"fmt"
	"sync"
	"time"
)

// TokenBucket 按固定速率补令牌，取到才放行
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

// Allow 先按流逝时间补令牌，再尝试取一个
func (b *TokenBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * b.rate
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func main() {
	b := NewTokenBucket(5, 2) // 容量 5，每秒补 2 个
	pass, reject := 0, 0
	for i := 0; i < 12; i++ {
		if b.Allow() {
			pass++
		} else {
			reject++
		}
	}
	fmt.Println("突发 12 个请求，放行:", pass, "拒绝:", reject)
	fmt.Println("限流后系统仍可服务:", pass > 0 && reject > 0)
}
