package main

import "fmt"

// 反例：两个 case 同时就绪时，select 并不总选书写在前的那个。
func main() {
	first, second := 0, 0
	for i := 0; i < 10000; i++ {
		a := make(chan int, 1)
		b := make(chan int, 1)
		a <- 1
		b <- 2
		select {
		case <-a:
			first++
		case <-b:
			second++
		}
	}
	fmt.Println("选到第一个 case 的次数 =", first)
	fmt.Println("选到第二个 case 的次数 =", second)
}
