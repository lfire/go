package main

import "fmt"

func main() {
	ch := make(chan int, 3) // 带 3 个槽位的缓冲 channel
	ch <- 10
	ch <- 20
	ch <- 30
	fmt.Println("len =", len(ch), "cap =", cap(ch)) // len 是当前排队元素数
	close(ch)                                       // 发送方关闭，缓冲里的数据仍在

	// range 会一直读到 channel 关闭且缓冲排空
	for v := range ch {
		fmt.Println("取出:", v)
	}
	v, ok := <-ch // 已关闭：立即返回零值与 false
	fmt.Printf("再次接收: v=%d ok=%v\n", v, ok)
}
