package main

import (
	"fmt"
	"runtime"
	"sync"
)

func main() {
	const n = 100000
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			// 每个 G 只做一件小事，返回后栈被回收、G 可被复用
			_ = 1 + 1
		}()
	}
	wg.Wait()
	fmt.Printf("成功创建并回收 %d 个 goroutine\n", n)
	fmt.Printf("当前仍存活: %d 个\n", runtime.NumGoroutine())
}
