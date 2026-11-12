package main

import (
	"testing"
	"time"
)

// 错误写法：命中很快，但每轮 time.After 都新建一个 timer，旧 timer 要等到 d 到期才回收
func loopAfter(fast chan struct{}, n int) {
	for i := 0; i < n; i++ {
		select {
		case <-fast:
			fast <- struct{}{}
		case <-time.After(time.Millisecond):
		}
	}
}

// 正确写法：只建一个 timer，命中后 Stop 再 Reset，反复复用
func loopReuse(fast chan struct{}, n int) {
	timer := time.NewTimer(time.Millisecond)
	defer timer.Stop()
	for i := 0; i < n; i++ {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(time.Millisecond)
		select {
		case <-fast:
			fast <- struct{}{}
		case <-timer.C:
		}
	}
}

func BenchmarkTimeAfter(b *testing.B) {
	fast := make(chan struct{}, 1)
	fast <- struct{}{}
	b.ResetTimer()
	loopAfter(fast, b.N)
}

func BenchmarkTimerReuse(b *testing.B) {
	fast := make(chan struct{}, 1)
	fast <- struct{}{}
	b.ResetTimer()
	loopReuse(fast, b.N)
}
