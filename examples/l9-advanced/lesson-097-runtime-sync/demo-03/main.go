package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// 观察三：高并发下锁竞争成为拐点，单一计数器用原子操作更划算。
func main() {
	const n = 100000
	var mu sync.Mutex
	var counter int64
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < n; j++ {
				mu.Lock()
				counter++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	lockCost := time.Since(start)

	var atomicCounter int64
	start = time.Now()
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < n; j++ {
				atomic.AddInt64(&atomicCounter, 1)
			}
		}()
	}
	wg.Wait()
	atomicCost := time.Since(start)

	fmt.Println("互斥锁结果 =", counter)
	fmt.Println("原子操作结果 =", atomic.LoadInt64(&atomicCounter))
	fmt.Println("互斥锁耗时 =", lockCost)
	fmt.Println("原子操作耗时 =", atomicCost)
}
