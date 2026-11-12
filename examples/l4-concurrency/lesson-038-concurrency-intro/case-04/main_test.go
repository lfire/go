package main

import (
	"os"
	"sync"
	"testing"
	"time"
)

// serialSum 串行累加，无共享写入，安全
func serialSum(n int) int {
	sum := 0
	for i := 0; i < n; i++ {
		sum++
	}
	return sum
}

// sharedSum 并发累加同一变量且无同步，构成数据竞争
func sharedSum(n int) int {
	sum := 0
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sum++
		}()
	}
	wg.Wait()
	return sum
}

func TestSerialSafe(t *testing.T) {
	if got := serialSum(1000); got != 1000 {
		t.Fatalf("串行期望 1000，实际 %d", got)
	}
}

// 反面实验：设 SHOW_RACE=1 并配合 -race，暴露共享累加的 DATA RACE
func TestSharedRace(t *testing.T) {
	if os.Getenv("SHOW_RACE") == "" {
		t.Skip("反面实验：设 SHOW_RACE=1 并加 -race 运行")
	}
	_ = sharedSum(1000)
}

// ioTask 模拟一次 100ms 的 I/O 等待，期间不占 CPU
func ioTask() { time.Sleep(100 * time.Millisecond) }

func BenchmarkSerialIO(b *testing.B) {
	for i := 0; i < b.N; i++ {
		for j := 0; j < 3; j++ {
			ioTask()
		}
	}
}

func BenchmarkConcurrentIO(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for j := 0; j < 3; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ioTask()
			}()
		}
		wg.Wait()
	}
}
