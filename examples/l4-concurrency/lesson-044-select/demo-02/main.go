package main

import (
	"fmt"
	"time"
)

func main() {
	// work 模拟慢任务，400ms 后回结果
	work := make(chan string, 1)
	go func() {
		time.Sleep(400 * time.Millisecond)
		work <- "任务完成"
	}()

	// 显式 NewTimer，结束时 Stop 释放底层计时器
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()

	select {
	case res := <-work:
		fmt.Println("收到结果：", res)
	case <-timer.C:
		fmt.Println("超时：100ms 内没等到结果")
	}
}
