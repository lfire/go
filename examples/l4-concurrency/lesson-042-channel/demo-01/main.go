package main

import (
	"fmt"
	"time"
)

func main() {
	ch := make(chan string) // 无缓冲：必须收发双方同时就绪
	go func() {
		fmt.Println("发送方: 准备发送")
		ch <- "hello" // 阻塞，直到有人接收
		fmt.Println("发送方: 对方已接走，我才继续")
	}()
	time.Sleep(100 * time.Millisecond) // 故意让接收方晚到
	fmt.Println("接收方: 开始接收")
	msg := <-ch
	fmt.Println("接收方: 收到", msg)
	time.Sleep(10 * time.Millisecond) // 给发送方打印收尾的机会
}
