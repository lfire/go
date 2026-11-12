package main

import (
	"os"
	"sync"
	"testing"
	"time"
)

// publishBad 用普通 bool 发布，写方与读方无 happens-before
func publishBad() int {
	var data int
	var ready bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		data = 42
		ready = true
	}()
	for i := 0; i < 1000 && !ready; i++ {
	}
	wg.Wait()
	return data
}

// publishOK 用 channel 发布，收发生成 happens-before
func publishOK() int {
	ch := make(chan int, 1)
	go func() {
		ch <- 42
	}()
	return <-ch
}

func TestPublish(t *testing.T) {
	for i := 0; i < 100; i++ {
		if got := publishOK(); got != 42 {
			t.Fatalf("期望 42，实际 %d", got)
		}
	}
}

// 反面实验：设 SHOW_RACE=1 并配合 -race，暴露 bool 发布的 DATA RACE
func TestPublishBadRace(t *testing.T) {
	if os.Getenv("SHOW_RACE") == "" {
		t.Skip("反面实验：设 SHOW_RACE=1 并加 -race 运行")
	}
	_ = publishBad()
}

// stopBad 用共享 bool 当停止标志，是数据竞争
func stopBad(stop *bool) {
	for !*stop {
		time.Sleep(time.Millisecond)
	}
}

// stopOK 用 close(done) 广播，关闭与接收建立 happens-before
func stopOK(done <-chan struct{}) {
	<-done
}

func TestStop(t *testing.T) {
	done := make(chan struct{})
	go stopOK(done)
	close(done)
}

func BenchmarkUnbuffered(b *testing.B) {
	ch := make(chan int)
	go func() {
		for i := 0; i < b.N; i++ {
			ch <- 1
		}
	}()
	for i := 0; i < b.N; i++ {
		<-ch
	}
}

func BenchmarkBuffered(b *testing.B) {
	ch := make(chan int, 64)
	for i := 0; i < b.N; i++ {
		ch <- 1
		<-ch
	}
}
