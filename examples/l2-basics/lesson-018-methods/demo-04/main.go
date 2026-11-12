package main

import (
	"fmt"
	"sync"
)

// SafeCounter 内含互斥锁，只能用指针接收者：
// 值接收者会把锁一并拷贝，两个副本各锁各的，等于没锁。
type SafeCounter struct {
	mu sync.Mutex
	n  int
}

func (c *SafeCounter) Add(delta int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n += delta
}

func (c *SafeCounter) Value() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func main() {
	var c SafeCounter
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Add(1)
		}()
	}
	wg.Wait()
	fmt.Println("并发累加结果 =", c.Value())
}
