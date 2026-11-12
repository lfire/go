// 文件：limiter.go —— 令牌桶稳态限流 + 一次性配额放号
package main

import (
	"fmt"
	"sync"
	"time"
)

// Limiter 是标准库实现的令牌桶：按速率补充令牌，把洪峰削成匀速。
type Limiter struct {
	mu       sync.Mutex
	rate     float64
	capacity float64
	tokens   float64
	last     time.Time
}

func NewLimiter(rate, capacity float64) *Limiter {
	return &Limiter{rate: rate, capacity: capacity, tokens: capacity, last: time.Now()}
}

func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.tokens += now.Sub(l.last).Seconds() * l.rate
	if l.tokens > l.capacity {
		l.tokens = l.capacity
	}
	l.last = now
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}

// Quota 是一次性令牌池：活动开始按库存乘以放大系数放号，抢完即止。
// 与 Limiter 的区别是不补水，专治秒杀这种一次性洪峰。
type Quota struct {
	mu   sync.Mutex
	left int
}

func NewQuota(n int) *Quota { return &Quota{left: n} }

func (q *Quota) Take() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.left <= 0 {
		return false
	}
	q.left--
	return true
}

func (q *Quota) Left() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.left
}

func main() {
	l := NewLimiter(10000, 10000)
	passed := 0
	for i := 0; i < 100000; i++ {
		if l.Allow() {
			passed++
		}
	}
	q := NewQuota(300)
	admitted := 0
	for i := 0; i < passed; i++ {
		if q.Take() {
			admitted++
		}
	}
	fmt.Printf("limiter passed=%d quota admitted=%d quota left=%d\n", passed, admitted, q.Left())
}
