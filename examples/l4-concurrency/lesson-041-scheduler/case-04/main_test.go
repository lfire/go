package main

import (
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"
)

var sink int

// tightLoop 没有函数调用与 channel 的紧密循环，Go 1.14 起由 sysmon 异步抢占
func tightLoop() {
	sum := 0
	for i := 0; i < 200_000_000; i++ {
		sum += i
	}
	sink = sum
}

func TestAsyncPreempt(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tightLoop()
	}()
	time.Sleep(10 * time.Millisecond)
	wg.Wait()
}

// blockingSyscall 用等待子进程制造一次真实的阻塞系统调用，触发 handoff
func blockingSyscall() {
	_ = exec.Command("sleep", "0.05").Run()
}

func burn(n int) int {
	sum := 0
	for i := 0; i < n; i++ {
		sum += i
	}
	return sum
}

func TestHandoff(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		blockingSyscall()
	}()
	time.Sleep(10 * time.Millisecond) // 确保 G1 已卡在内核
	go func() {
		defer wg.Done()
		_ = burn(5_000_000) // P 被 handoff 后仍能跑
	}()
	wg.Wait()
}

// netIO 走 netpoller，G 挂起而 M 不阻塞
func netIO(n int) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(20 * time.Millisecond)
		}()
	}
	wg.Wait()
}

func BenchmarkNetIO(b *testing.B) {
	for i := 0; i < b.N; i++ {
		netIO(50)
	}
}

func BenchmarkBlockingSyscall(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for j := 0; j < 50; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = exec.Command("sleep", "0.02").Run()
			}()
		}
		wg.Wait()
	}
}
