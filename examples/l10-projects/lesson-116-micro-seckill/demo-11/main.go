// 文件：atomic_deduct.go —— 坑一：先查后扣超卖，原子临界区修复
package main

import (
	"fmt"
	"sync"
)

type store struct {
	mu   sync.Mutex
	left int
}

func (s *store) read() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.left
}

// deductWrong 是错误写法：读与写分成两步，并发下都读到旧值。
func (s *store) deductWrong() bool {
	if s.read() > 0 {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.left--
		return true
	}
	return false
}

// deductRight 把检查与扣减合成一个临界区，等价于
// UPDATE stock SET available=available-1 WHERE available 大于 0 的影响行数。
func (s *store) deductRight() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.left <= 0 {
		return false
	}
	s.left--
	return true
}

func run(deduct func(*store) bool) int {
	s := &store{left: 100}
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			deduct(s)
		}()
	}
	wg.Wait()
	return s.left
}

func main() {
	fmt.Println("wrong final left:", run((*store).deductWrong))
	fmt.Println("right final left:", run((*store).deductRight))
}
