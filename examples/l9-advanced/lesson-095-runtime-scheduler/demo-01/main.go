package main

import (
	"fmt"
	"runtime"
	"sync/atomic"
)

// 观察一：GOMAXPROCS 决定并行度，NumGoroutine 统计当前存活的 G。
func main() {
	fmt.Println("默认 P 数 =", runtime.GOMAXPROCS(0))
	runtime.GOMAXPROCS(2)
	fmt.Println("调整后 P 数 =", runtime.GOMAXPROCS(0))

	var counter int64
	const n = 8
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			atomic.AddInt64(&counter, 1)
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}
	fmt.Println("完成计数 =", atomic.LoadInt64(&counter))
	fmt.Println("收尾后 G 数 =", runtime.NumGoroutine())
}
