package main

import "fmt"

func sum(nums ...int) int { // 变参：调用时可以给任意多个 int
	total := 0
	for _, n := range nums {
		total += n
	}
	return total
}

func apply(f func(int) int, x int) int { // 函数本身也能当参数
	return f(x)
}

func double(n int) int { return n * 2 }

func main() {
	fmt.Println("求和：", sum(1, 2, 3, 4))
	fmt.Println("把函数传进去：", apply(double, 21))
}
