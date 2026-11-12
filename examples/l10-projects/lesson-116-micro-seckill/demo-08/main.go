// 文件：circuit.go —— 熔断器状态机与降级兜底
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("circuit: open")

type state uint8

const (
	closed state = iota
	open
	halfOpen
)

// Breaker 统计连续失败，超阈值打开熔断；冷却后半开试探，
// 试探成功回到 closed，失败重新 open。
type Breaker struct {
	mu        sync.Mutex
	state     state
	failures  int
	threshold int
	openUntil time.Time
	cooldown  time.Duration
	now       func() time.Time
}

func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	return &Breaker{state: closed, threshold: threshold, cooldown: cooldown, now: time.Now}
}

func (b *Breaker) Allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case open:
		if b.now().Before(b.openUntil) {
			return ErrCircuitOpen
		}
		b.state = halfOpen
	case halfOpen:
		return ErrCircuitOpen // 半开只放一个试探请求
	}
	return nil
}

func (b *Breaker) Report(ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ok {
		b.failures = 0
		b.state = closed
		return
	}
	b.failures++
	if b.failures >= b.threshold || b.state == halfOpen {
		b.state = open
		b.openUntil = b.now().Add(b.cooldown)
	}
}

func (b *Breaker) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return [...]string{"closed", "open", "half-open"}[b.state]
}

func main() {
	b := NewBreaker(3, 50*time.Millisecond)
	for i := 0; i < 3; i++ {
		b.Report(false)
	}
	fmt.Println("after 3 failures:", b.State(), "allow:", b.Allow())
	time.Sleep(60 * time.Millisecond)
	fmt.Println("after cooldown allow:", b.Allow(), "state:", b.State())
	b.Report(true)
	fmt.Println("after probe success:", b.State())
}
