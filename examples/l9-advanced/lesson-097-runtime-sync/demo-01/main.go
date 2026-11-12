package main

import (
	"fmt"
	"runtime"
	"time"
)

// 观察一：无缓冲 channel 的发送者会挂到 sendq，直到接收者出现。
func main() {
	ch := make(chan int)
	sent := make(chan struct{})
	go func() {
		ch <- 42 // 无接收者时阻塞，进入 hchan.sendq
		close(sent)
	}()

	time.Sleep(20 * time.Millisecond)
	fmt.Println("发送阻塞期间的 G 数 =", runtime.NumGoroutine())
	select {
	case <-sent:
		fmt.Println("不应发生：接收前就发送成功")
	default:
		fmt.Println("发送仍阻塞，符合预期")
	}
	fmt.Println("收到值 =", <-ch)
	<-sent
	fmt.Println("接收后 G 数 =", runtime.NumGoroutine())
}
