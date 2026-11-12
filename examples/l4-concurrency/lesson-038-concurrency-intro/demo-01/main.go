package main

import (
	"fmt"
	"sync"
	"time"
)

// task 模拟一次耗时 100ms 的 I/O 操作，期间不占用 CPU
func task(id int) {
	time.Sleep(100 * time.Millisecond)
	fmt.Printf("任务 %d 完成\n", id)
}

func main() {
	// 串行：三个任务一个接一个，等待时间累加
	start := time.Now()
	for i := 1; i <= 3; i++ {
		task(i)
	}
	fmt.Printf("串行耗时: %v\n", time.Since(start).Round(time.Millisecond))

	// 并发：三个任务同时开始，等待时间重叠
	start = time.Now()
	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			task(id)
		}(i)
	}
	wg.Wait()
	fmt.Printf("并发耗时: %v\n", time.Since(start).Round(time.Millisecond))
}
