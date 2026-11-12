package main

import (
	"fmt"
	"runtime"
	"sync"
)

// 观察三：本地队列 runq 容量是 256，一次就绪超过这个数量，
// 多余的 G 会溢出到 schedt.runq 全局队列，由 schedtrace 可见。
func main() {
	fmt.Println("P 数 =", runtime.GOMAXPROCS(0))
	const n = 600
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
		}()
	}
	fmt.Println("阻塞中的 G 数（含 main）=", runtime.NumGoroutine())
	close(start) // 600 个 G 同时变为可运行
	wg.Wait()
	fmt.Println("全部结束后 G 数 =", runtime.NumGoroutine())
}
