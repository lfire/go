package main

import "fmt"

type payload struct{ n int }

func main() {
	ch := make(chan payload) // 无缓冲
	done := make(chan struct{})
	go func() {
		p := payload{n: 7}
		ch <- p       // 运行时把 p 直接拷贝到接收方的变量上
		p.n = 999     // 交接后再改发送方本地变量，不影响已交出去的值
		fmt.Println("发送方修改后:", p.n)
		close(done)
	}()
	got := <-ch
	<-done // 等发送方收尾，保证输出顺序
	fmt.Println("接收方拿到:", got.n)
}
