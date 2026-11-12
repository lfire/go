package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

type Breaker struct {
	mu           sync.Mutex
	state        State
	failures     int
	threshold    int
	openUntil    time.Time
	cooldown     time.Duration
	halfOpenMax  int
	halfOpenSucc int
}

func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	return &Breaker{state: Closed, threshold: threshold, cooldown: cooldown, halfOpenMax: 2}
}

var ErrOpen = errors.New("circuit open")

// Allow 在调用前判定是否放行；Open 期间直接短路，冷却到期转 HalfOpen。
func (b *Breaker) Allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case Open:
		if time.Now().Before(b.openUntil) {
			return ErrOpen
		}
		b.state = HalfOpen
		b.halfOpenSucc = 0
		return nil
	default:
		return nil
	}
}

// Record 在调用后记录成功或失败，驱动状态迁移。
func (b *Breaker) Record(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err == nil {
		switch b.state {
		case HalfOpen:
			b.halfOpenSucc++
			if b.halfOpenSucc >= b.halfOpenMax {
				b.state = Closed
				b.failures = 0
			}
		default:
			b.failures = 0
		}
		return
	}
	switch b.state {
	case HalfOpen:
		b.trip() // 探测仍失败，立刻重新熔断
	default:
		b.failures++
		if b.failures >= b.threshold {
			b.trip()
		}
	}
}

func (b *Breaker) trip() {
	b.state = Open
	b.openUntil = time.Now().Add(b.cooldown)
	b.failures = 0
}

func (b *Breaker) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return [...]string{"Closed", "Open", "HalfOpen"}[b.state]
}

func main() {
	b := NewBreaker(3, 300*time.Millisecond)
	// 连续 3 次失败，触发熔断
	for i := 0; i < 3; i++ {
		if err := b.Allow(); err == nil {
			b.Record(fmt.Errorf("downstream 503"))
		}
	}
	fmt.Println("失败后状态:", b.State(), "再次调用:", b.Allow())

	// 冷却到期后转半开，探测成功才恢复
	time.Sleep(320 * time.Millisecond)
	_ = b.Allow()
	b.Record(nil)
	_ = b.Allow()
	b.Record(nil)
	fmt.Println("探测恢复后状态:", b.State())
}
