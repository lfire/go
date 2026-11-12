package main

import (
	"os"
	"sync"
	"testing"
)

// 错误对照：用普通 bool 发布数据，写方与读方无 happens-before
func publishBad() int {
	var data int
	var ready bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		data = 42
		ready = true // 与读方的读无同步
	}()
	for i := 0; i < 1000 && !ready; i++ {
	}
	wg.Wait()
	return data
}

// 正确：用 channel 发布，收发生成 happens-before
func publishOK() int {
	ready := make(chan int, 1)
	go func() {
		ready <- 42
	}()
	return <-ready
}

func TestPublish(t *testing.T) {
	for i := 0; i < 100; i++ {
		if got := publishOK(); got != 42 {
			t.Fatalf("期望 42，实际 %d", got)
		}
	}
}

// 反面实验：设 SHOW_RACE=1 并配合 -race，可看到 bool 发布的 DATA RACE
func TestPublishBadRace(t *testing.T) {
	if os.Getenv("SHOW_RACE") == "" {
		t.Skip("反面实验：设 SHOW_RACE=1 并加 -race 运行")
	}
	_ = publishBad()
}
