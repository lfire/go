package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Event struct {
	TS     int64
	Status int
	Bytes  int
}

// Stat 汇总下游处理进度，多个阶段并发读写时用互斥锁保护。
type Stat struct {
	mu    sync.Mutex
	count int64
	bytes int64
}

// boundedPipe 用有界 channel 把下游能力反馈成上游等待：缓冲满时 send 阻塞，
// 内存不会随流量无限增长；ctx.Done 让取消穿透 send 阻塞，避免 goroutine 泄漏。
func boundedPipe(ctx context.Context, in <-chan Event, capacity int) <-chan Event {
	out := make(chan Event, capacity)
	go func() {
		defer close(out)
		for e := range in {
			select {
			case out <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// slowSink 模拟每处理一条要 2ms 的慢下游。
func slowSink(ctx context.Context, in <-chan Event, st *Stat) {
	for e := range in {
		select {
		case <-ctx.Done():
			return
		default:
		}
		time.Sleep(2 * time.Millisecond)
		st.mu.Lock()
		st.count++
		st.bytes += int64(e.Bytes)
		st.mu.Unlock()
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	src := make(chan Event)
	staged := boundedPipe(ctx, src, 8)
	st := &Stat{}
	done := make(chan struct{})
	go func() {
		slowSink(ctx, staged, st)
		close(done)
	}()
	go func() {
		defer close(src)
		for i := 0; i < 100000; i++ {
			select {
			case src <- Event{TS: int64(i), Status: 200, Bytes: 128}:
			case <-ctx.Done():
				return
			}
		}
	}()
	<-done
	st.mu.Lock()
	fmt.Printf("200ms 内处理=%d 条 字节=%d（背压使内存有界、随 ctx 及时退出）\n", st.count, st.bytes)
	st.mu.Unlock()
}
