package main

import (
	"fmt"
	"time"
)

func worker(done <-chan struct{}, jobs <-chan int) {
	for {
		// 第一层：非阻塞优先检查高优先级停止信号
		select {
		case <-done:
			fmt.Println("收到停止信号，退出")
			return
		default:
		}
		// 第二层：在停止与任务之间多路复用
		select {
		case <-done:
			fmt.Println("收到停止信号，退出")
			return
		case j := <-jobs:
			fmt.Println("处理任务", j)
		}
	}
}

func main() {
	done := make(chan struct{})
	jobs := make(chan int)
	go worker(done, jobs)
	for j := 1; j <= 3; j++ {
		jobs <- j
	}
	time.Sleep(50 * time.Millisecond)
	close(done) // 关闭是广播，所有接收者立即返回
	time.Sleep(50 * time.Millisecond)
	fmt.Println("main 结束")
}
