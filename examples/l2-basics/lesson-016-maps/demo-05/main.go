package main

import (
	"errors"
	"fmt"
	"sync"
)

// SafeIndex 用读写锁保护 map：读多写少时多个读可并行。
type SafeIndex struct {
	mu sync.RWMutex
	m  map[string]int
}

func NewSafeIndex() *SafeIndex {
	return &SafeIndex{m: make(map[string]int)}
}

func (s *SafeIndex) Set(k string, v int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
}

// Get 查不到时返回明确错误，不把零值误当成命中
func (s *SafeIndex) Get(k string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.m[k]; ok {
		return v, nil
	}
	return 0, errors.New("键不存在")
}

func main() {
	idx := NewSafeIndex()
	idx.Set("a", 0) // 值恰好为 0 但键真实存在
	v, err := idx.Get("a")
	fmt.Println("命中零值 =", v, "出错 =", err != nil)

	_, err = idx.Get("missing")
	fmt.Println("缺失时出错 =", err != nil, "提示 =", err)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) { // 把循环变量作参数传入，避开闭包捕获
			defer wg.Done()
			idx.Set(fmt.Sprintf("k%d", n), n)
		}(i)
	}
	wg.Wait()
	v, err = idx.Get("k7")
	fmt.Println("并发写后取 k7 =", v, "出错 =", err != nil)
}
