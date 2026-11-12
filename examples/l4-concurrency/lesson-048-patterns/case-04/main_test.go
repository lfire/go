package main

import (
	"testing"
	"time"
)

// 错误写法：生产阶段忘了 close(out)，消费方 range 永远阻塞
func genNoClose(nums []int) <-chan int {
	out := make(chan int)
	go func() {
		for _, n := range nums {
			out <- n
		}
		// 漏掉 close(out)
	}()
	return out
}

// 正确写法：defer close(out)，下游 range 才能结束
func genOK(nums []int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for _, n := range nums {
			out <- n
		}
	}()
	return out
}

func TestPipelineClose(t *testing.T) {
	total := 0
	for n := range genOK([]int{1, 2, 3}) {
		total += n
	}
	if total != 6 {
		t.Fatalf("期望 6，实际 %d", total)
	}
}

func TestNoCloseLeaks(t *testing.T) {
	ch := genNoClose([]int{1, 2, 3, 4, 5})
	done := make(chan struct{})
	go func() {
		for range ch { // 永远收不到关闭信号
		}
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("不该结束：漏关 channel 时 range 应永久阻塞")
	case <-time.After(50 * time.Millisecond):
		// 预期：消费 goroutine 泄漏，可用 runtime.NumGoroutine 或 goleak 观察
	}
}
