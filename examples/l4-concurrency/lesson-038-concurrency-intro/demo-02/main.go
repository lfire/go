package main

import "fmt"

// produce 是唯一的生产者，只通过参数里的 channel 把数据交出去
func produce(ch chan<- int) {
	for i := 1; i <= 3; i++ {
		ch <- i * i // 发送完成，这个值的所有权就转移给了接收方
	}
	close(ch) // 发送方负责关闭，通知接收方没有更多数据
}

func main() {
	ch := make(chan int)
	go produce(ch)
	// 主 goroutine 不碰任何共享内存，只从通道里取
	for v := range ch {
		fmt.Println("收到:", v)
	}
}
