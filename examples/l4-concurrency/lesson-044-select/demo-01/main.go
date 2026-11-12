package main

import "fmt"

func main() {
	// 两个带缓冲 channel，让多个 case 同时就绪
	a := make(chan string, 2)
	b := make(chan string, 2)
	a <- "A1"
	a <- "A2"
	b <- "B1"
	b <- "B2"

	hitA, hitB := 0, 0
	for i := 0; i < 6; i++ { // 共 4 个值，后 2 轮走 default
		select {
		case v := <-a:
			fmt.Println("读 a：", v)
			hitA++
		case v := <-b:
			fmt.Println("读 b：", v)
			hitB++
		default:
			fmt.Println("都空，执行 default，不阻塞")
		}
	}
	fmt.Printf("a 命中 %d 次，b 命中 %d 次\n", hitA, hitB)
}
