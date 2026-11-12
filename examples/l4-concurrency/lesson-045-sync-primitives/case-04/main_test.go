package main

import (
	"os"
	"sync"
	"testing"
)

type Counter struct {
	mu sync.Mutex
	n  int
}

// 错误对照：读路径没进临界区，与写无同步
func (c *Counter) badGet() int { return c.n }

// 正确：读写都由同一把锁覆盖
func (c *Counter) Add() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *Counter) Get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func TestGuard(t *testing.T) {
	var c Counter
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1) // 计数先登记，Wait 才不会漏任务
		go func() {
			defer wg.Done()
			c.Add()
		}()
	}
	wg.Wait()
	if got := c.Get(); got != 1000 {
		t.Fatalf("期望 1000，实际 %d", got)
	}
}

// 反面实验：设 SHOW_RACE=1 并配合 -race，可看到读未加锁的 DATA RACE
func TestBadGuardRace(t *testing.T) {
	if os.Getenv("SHOW_RACE") == "" {
		t.Skip("反面实验：设 SHOW_RACE=1 并加 -race 运行")
	}
	var c Counter
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			c.Add()
		}
	}()
	for i := 0; i < 1000; i++ {
		_ = c.badGet() // 与写并发且无锁
	}
	wg.Wait()
}
