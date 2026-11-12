package main

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

// leaking 启动一个永远等不到数据的 goroutine，制造泄漏
func leaking() <-chan struct{} {
	blocked := make(chan struct{})
	go func() {
		<-blocked // 无人 close，永久 gopark
	}()
	return blocked
}

// correct 用 close 广播给阻塞的 G 一个明确的退出路径
func correct() {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-done
	}()
	close(done) // 关闭即广播，与接收建立 happens-before
	wg.Wait()
}

func TestExitPath(t *testing.T) {
	before := runtime.NumGoroutine()
	correct()
	time.Sleep(10 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("存在未回收的 G：前 %d 后 %d", before, after)
	}
}

func BenchmarkSpawnG(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done() }()
		wg.Wait()
	}
}
