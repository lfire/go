package main

import (
	"fmt"
	"sync"
)

func main() {
	var (
		mu      sync.Mutex
		counter int
		wg      sync.WaitGroup
	)
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			counter++ // 临界区独占访问
			mu.Unlock()
		}()
	}
	wg.Wait()
	fmt.Println("期望 1000，实际：", counter) // 稳定 1000
}
