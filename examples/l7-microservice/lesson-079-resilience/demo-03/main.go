package main

import (
	"errors"
	"fmt"
	"time"
)

// Bulkhead 用带缓冲 channel 当信号量，把某个下游的故障关在独立舱内。
type Bulkhead struct{ sem chan struct{} }

func NewBulkhead(n int) *Bulkhead { return &Bulkhead{sem: make(chan struct{}, n)} }

var ErrBusy = errors.New("bulkhead full")

func (b *Bulkhead) Do(fn func() (string, error)) (string, error) {
	select {
	case b.sem <- struct{}{}:
		defer func() { <-b.sem }()
		return fn()
	default:
		return "", ErrBusy // 配额满，立即失败而非排队
	}
}

// CallWithFallback 把下游调用与降级兜底收在一处：忙或失败都返回兜底值。
func CallWithFallback(b *Bulkhead, fallback string) string {
	out, err := b.Do(func() (string, error) {
		time.Sleep(50 * time.Millisecond) // 模拟慢下游
		return "real-data", nil
	})
	if err != nil {
		return fallback
	}
	return out
}

func main() {
	b := NewBulkhead(1)
	b.sem <- struct{}{} // 手动占满唯一配额，模拟慢下游把舱占住
	fmt.Println("舱壁满时返回:", CallWithFallback(b, "cached-data"))
	<-b.sem // 释放配额
	fmt.Println("恢复后返回:", CallWithFallback(b, "cached-data"))
}
