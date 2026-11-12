package main

import (
	"context"
	"fmt"
	"time"
)

type ExecFn func(ctx context.Context) (string, error)

type outcome struct {
	log string
	err error
}

func runIsolated(ctx context.Context, fn ExecFn) (string, error) {
	ch := make(chan outcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- outcome{err: fmt.Errorf("任务 panic: %v", r)}
			}
		}()
		log, err := fn(ctx)
		ch <- outcome{log: log, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case o := <-ch:
		return o.log, o.err
	}
}

// Pool 用带缓冲 channel 作信号量限制并发（舱壁隔离）。
type Pool struct{ sem chan struct{} }

func NewPool(n int) *Pool { return &Pool{sem: make(chan struct{}, n)} }

func (p *Pool) Submit(ctx context.Context, timeout time.Duration, fn ExecFn) (string, error) {
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-p.sem }()
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return runIsolated(c, fn)
}

func main() {
	p := NewPool(2)
	fast := func(ctx context.Context) (string, error) { return "ok", nil }
	slow := func(ctx context.Context) (string, error) {
		select {
		case <-time.After(200 * time.Millisecond):
			return "late", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	boom := func(ctx context.Context) (string, error) { panic("bad job") }
	for _, tc := range []struct {
		name string
		fn   ExecFn
		to   time.Duration
	}{
		{"正常", fast, time.Second},
		{"超时", slow, 20 * time.Millisecond},
		{"panic", boom, time.Second},
	} {
		log, err := p.Submit(context.Background(), tc.to, tc.fn)
		fmt.Printf("%-6s log=%q err=%v\n", tc.name, log, err)
	}
}
