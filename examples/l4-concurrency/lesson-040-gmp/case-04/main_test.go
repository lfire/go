package main

import (
	"os"
	"runtime"
	"sync"
	"testing"
)

// burn 纯计算，会持续占住一个 P
func burn(n int) int {
	sum := 0
	for i := 0; i < n; i++ {
		sum += i
	}
	return sum
}

func runParallel(procs, workers int) int {
	old := runtime.GOMAXPROCS(procs)
	defer runtime.GOMAXPROCS(old)
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := burn(20_000_000)
			mu.Lock()
			total += v
			mu.Unlock()
		}()
	}
	wg.Wait()
	return total
}

func TestParallelSum(t *testing.T) {
	if got := runParallel(2, 2); got == 0 {
		t.Fatal("计算结果为 0，异常")
	}
}

// 反面实验：单 P 下两个 G 交替写共享变量且无同步，仍构成数据竞争
func scheduleIsNotSync() int {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	n := 0
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				n++
			}
		}()
	}
	wg.Wait()
	return n
}

func TestScheduleNotSyncRace(t *testing.T) {
	if os.Getenv("SHOW_RACE") == "" {
		t.Skip("反面实验：设 SHOW_RACE=1 并加 -race 运行")
	}
	_ = scheduleIsNotSync()
}

func BenchmarkP1(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = runParallel(1, 2)
	}
}

func BenchmarkPN(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = runParallel(runtime.NumCPU(), 2)
	}
}
