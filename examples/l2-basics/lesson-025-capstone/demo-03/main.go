package main

import (
	"fmt"
	"sync"
)

// safeKV 只保留最小的一对方法，专门验证锁的作用。
type safeKV struct {
	mu sync.RWMutex
	m  map[string]string
}

func (s *safeKV) Put(k, v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
}

func (s *safeKV) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

func main() {
	s := &safeKV{m: make(map[string]string)}
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.Put(fmt.Sprintf("k%d", i), "v")
		}(i)
	}
	wg.Wait()
	fmt.Println(s.Size()) // 1000
}
