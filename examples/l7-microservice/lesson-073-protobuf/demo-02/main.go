package main

import "fmt"

// 用 channel 模拟 gRPC 四种流：核心差别是请求与响应的个数以及谁先关流。
func main() {
	// 1. Unary：一个请求，一个响应
	fmt.Println("Unary:", unary(3, 4))

	// 2. Server streaming：一个请求，服务端推一串响应后关流
	fmt.Print("server-stream: ")
	for v := range serverStream("go") {
		fmt.Print(v, " ")
	}
	fmt.Println()

	// 3. Client streaming：客户端推一串请求，服务端在流结束时汇总成一个响应
	fmt.Println("client-stream sum:", clientStream([]int{1, 2, 3, 4}))

	// 4. Bidi streaming：两边各推各的，边收边发，互不等待
	fmt.Print("bidi: ")
	bidi()
}

func unary(a, b int) int { return a + b }

func serverStream(prefix string) <-chan string {
	ch := make(chan string)
	go func() {
		defer close(ch) // 服务端推完即关流，客户端用 range 感知结束
		for _, s := range []string{"hello", "world"} {
			ch <- prefix + "-" + s
		}
	}()
	return ch
}

func clientStream(nums []int) int {
	sum := 0
	for _, n := range nums { // 客户端逐个写入，流结束后才拿到汇总响应
		sum += n
	}
	return sum
}

func bidi() {
	requests := make(chan int)
	responses := make(chan int)
	go func() { // 服务端：来一个算一个，边收边回
		defer close(responses)
		for n := range requests {
			responses <- n * n
		}
	}()
	go func() { // 客户端：边发边收，不必等对方全部结束
		for _, n := range []int{1, 2, 3} {
			requests <- n
		}
		close(requests)
	}()
	for r := range responses {
		fmt.Print(r, " ")
	}
	fmt.Println()
}
