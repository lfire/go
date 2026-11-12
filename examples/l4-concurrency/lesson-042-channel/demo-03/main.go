package main

import "fmt"

// produce 只负责发送，签名把意图写死在类型里
func produce(out chan<- int) {
	for i := 1; i <= 3; i++ {
		out <- i
	}
	close(out) // 唯一的发送方负责关闭
}

// consume 只负责接收
func consume(in <-chan int) {
	for v := range in {
		fmt.Println("消费:", v)
	}
}

func main() {
	ch := make(chan int)
	go produce(ch)
	consume(ch)

	// nil channel：零值 channel 上的收发会永久阻塞
	var nilCh chan int
	fmt.Println("nilCh == nil ?", nilCh == nil)

	// 重复关闭会 panic，这里用 recover 演示（真实代码不要靠 recover）
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("捕获到 panic:", r)
		}
	}()
	close(ch) // ch 已经在 produce 里被关闭
}
