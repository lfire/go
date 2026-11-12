package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

func main() {
	const tasks = 1000
	var done atomic.Int64 // Go 1.19 起提供，1.21 完全可用
	jobs := make(chan int, 64)

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs { // 每个 worker 循环取任务，直到 channel 关闭
				done.Add(1)
			}
		}()
	}
	for i := 0; i < tasks; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	fmt.Printf("P 数=%d，worker 数=8，处理任务数=%d\n", runtime.GOMAXPROCS(0), done.Load())
}
