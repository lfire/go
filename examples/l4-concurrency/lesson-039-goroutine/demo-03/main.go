package main

import (
	"fmt"
	"runtime"
	"time"
)

// leak 故意启动一个永远等不到数据的 goroutine
func leak() {
	ch := make(chan int)
	go func() {
		v := <-ch // 没有任何发送者，这里永久阻塞
		fmt.Println("永远不会执行:", v)
	}()
}

func main() {
	fmt.Println("启动前 goroutine 数:", runtime.NumGoroutine())
	leak()
	time.Sleep(10 * time.Millisecond) // 给调度器时间把新 G 挂起
	fmt.Println("泄漏后 goroutine 数:", runtime.NumGoroutine())
	// 排查手段：pprof 的 goroutine profile、go tool trace，
	// 以及给每一条阻塞路径配上超时或取消信号。
}
