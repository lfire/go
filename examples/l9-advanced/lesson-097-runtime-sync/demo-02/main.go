package main

import (
	"fmt"
	"runtime"
	"time"
)

// 观察二：带缓冲 channel 用环形队列 buf 承载元素，len(ch) 即 hchan.qcount。
func main() {
	ch := make(chan string, 3)
	ch <- "a"
	ch <- "b"
	ch <- "c"
	fmt.Println("已入队元素数 =", len(ch))
	fmt.Println("队列容量 =", cap(ch))

	blocked := make(chan struct{})
	go func() {
		ch <- "d" // 缓冲区满，挂到 sendq
		close(blocked)
	}()
	time.Sleep(20 * time.Millisecond)
	fmt.Println("满队列后阻塞的 G 数 =", runtime.NumGoroutine())
	fmt.Println("取出 =", <-ch)
	<-blocked
	fmt.Println("取出后队列长度 =", len(ch))
}
