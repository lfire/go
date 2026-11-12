package main

import "fmt"

// 观察二：小函数满足内联预算会被摊平；noinline 指令可强制对照。
func add(a, b int) int { return a + b }

//go:noinline
func noInline(a, b int) int { return a + b }

func main() {
	fmt.Println(add(1, 2))
	fmt.Println(noInline(3, 4))
}
