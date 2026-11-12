package main

import (
	"fmt"
	"sync"
)

type SafeMap struct {
	mu sync.RWMutex // 读多写少时更划算
	m  map[string]int
}

func (s *SafeMap) Add(k string, v int) {
	s.mu.Lock()         // 写锁独占
	defer s.mu.Unlock() // 保证任何路径都解锁
	s.m[k] = s.m[k] + v
}

func (s *SafeMap) Get(k string) int {
	s.mu.RLock()         // 多个读可并行
	defer s.mu.RUnlock()
	return s.m[k]
}

func main() {
	s := &SafeMap{m: make(map[string]int)}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1) // Add 必须在启动 goroutine 之前
		go func() {
			defer wg.Done()
			s.Add("count", 1)
		}()
	}
	wg.Wait()
	fmt.Println("count =", s.Get("count"))
}
