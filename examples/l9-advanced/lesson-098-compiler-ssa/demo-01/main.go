package main

import "fmt"

// 观察一：两个局部变量分别留在栈上或逃逸到堆，用 -gcflags=-m 验证。
func noEscape() int {
	x := 42
	return x
}

func doesEscape() *int {
	y := 42
	return &y // 指针被返回，y 必须逃逸到堆
}

func main() {
	fmt.Println(noEscape())
	fmt.Println(*doesEscape())
}
