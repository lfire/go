// 文件：internal/gateway/resilience.go
package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// BreakerState 是熔断器三态。
type BreakerState int

const (
	StateClosed BreakerState = iota
	StateOpen
	StateHalfOpen
)

// CircuitBreaker 按连续失败数在三个状态间迁移。
type CircuitBreaker struct {
	mu          sync.Mutex
	state       BreakerState
	failures    int
	successes   int
	threshold   int
	openUntil   time.Time
	openFor     time.Duration
	halfOpenMax int
}

func NewCircuitBreaker(threshold int, openFor time.Duration, halfOpenMax int) *CircuitBreaker {
	return &CircuitBreaker{threshold: threshold, openFor: openFor, halfOpenMax: halfOpenMax}
}

func (cb *CircuitBreaker) allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case StateOpen:
		if time.Now().Before(cb.openUntil) {
			return false
		}
		cb.state = StateHalfOpen
		cb.successes = 0
		return true
	case StateHalfOpen:
		return cb.successes < cb.halfOpenMax
	default:
		return true
	}
}

func (cb *CircuitBreaker) report(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if err != nil {
		cb.failures++
		if cb.state == StateHalfOpen || cb.failures >= cb.threshold {
			cb.state = StateOpen
			cb.openUntil = time.Now().Add(cb.openFor)
		}
		return
	}
	cb.failures = 0
	if cb.state == StateHalfOpen {
		cb.successes++
		if cb.successes >= cb.halfOpenMax {
			cb.state = StateClosed
		}
	}
}

func (cb *CircuitBreaker) State() BreakerState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// StreamResult 的 FirstChunk 决定这次失败能不能重试。
type StreamResult struct {
	FirstChunk bool
	Err        error
}

// CallWithTimeout 用首字节超时加块间空闲超时替代整体超时。
func CallWithTimeout(ctx context.Context, first, idle time.Duration,
	produce func(ctx context.Context, emit func(string)) error) StreamResult {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	firstTimer := time.NewTimer(first)
	idleTimer := time.NewTimer(idle)
	defer firstTimer.Stop()
	defer idleTimer.Stop()
	var firstSeen atomic.Bool
	progress := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- produce(ctx, func(string) {
			firstSeen.CompareAndSwap(false, true)
			select {
			case progress <- struct{}{}: // 通知主循环，timer 只在主循环里 Reset
			default:
			}
		})
	}()
	var res StreamResult
	for {
		select {
		case <-ctx.Done():
			res.Err = ctx.Err()
			res.FirstChunk = firstSeen.Load()
			return res
		case <-progress:
			if firstSeen.Load() {
				firstTimer.Stop()
			}
			idleTimer.Reset(idle) // 每来一块就重置空闲计时
		case <-firstTimer.C:
			if !firstSeen.Load() {
				cancel()
				res.Err = errors.New("first byte timeout")
				return res
			}
		case <-idleTimer.C:
			cancel()
			res.Err = errors.New("idle timeout")
			res.FirstChunk = firstSeen.Load()
			return res
		case err := <-done:
			res.Err = err
			res.FirstChunk = firstSeen.Load()
			return res
		}
	}
}

// Deliver 把失败分流：首字节前可重试，半流失败绝不重试。
func Deliver(ctx context.Context, cb *CircuitBreaker, attempts int,
	produce func(ctx context.Context, emit func(string)) error) StreamResult {
	var last StreamResult
	for i := 0; i < attempts; i++ {
		if !cb.allow() {
			return StreamResult{Err: errors.New("circuit open")}
		}
		res := CallWithTimeout(ctx, 2*time.Second, 5*time.Second, produce)
		cb.report(res.Err)
		last = res
		if res.Err == nil {
			return res
		}
		if res.FirstChunk {
			return res // 半流失败：重试会重复输出并翻倍计费
		}
		time.Sleep(time.Duration(rand.Intn(20)) * time.Millisecond) // 带抖动退避
	}
	return last
}

func main() {
	cb := NewCircuitBreaker(3, 100*time.Millisecond, 2)
	// 场景一：持续出块，总时长超过首字节超时但不被截断
	res := Deliver(context.Background(), cb, 2, func(ctx context.Context, emit func(string)) error {
		for i := 0; i < 3; i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(800 * time.Millisecond):
			}
			emit("token")
		}
		return nil
	})
	fmt.Println("normal ok:", res.Err == nil, "first:", res.FirstChunk)
	// 场景二：上游始终不出块，触发首字节超时并重试，最终熔断
	res2 := Deliver(context.Background(), cb, 4, func(ctx context.Context, emit func(string)) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
		emit("late")
		return nil
	})
	fmt.Println("timeout:", res2.Err, "first:", res2.FirstChunk, "state:", cb.State())
	// 场景三：context 取消传播到 produce
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res3 := CallWithTimeout(ctx, time.Second, time.Second, func(ctx context.Context, emit func(string)) error {
		<-ctx.Done()
		return ctx.Err()
	})
	fmt.Println("cancelled:", res3.Err)
}
