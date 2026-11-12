package main

import "fmt"

// gen 生产阶段：送数后关闭
func gen(nums ...int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out) // 必须关闭，否则下游 range 不结束
		for _, n := range nums {
			out <- n
		}
	}()
	return out
}

// square 加工阶段：读 in、平方后写 out
func square(in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for n := range in { // in 关闭时自动退出
			out <- n * n
		}
	}()
	return out
}

func main() {
	// 串成管道：gen 接 square，main 消费输出
	for s := range square(gen(1, 2, 3, 4, 5)) {
		fmt.Println("平方：", s)
	}
}
