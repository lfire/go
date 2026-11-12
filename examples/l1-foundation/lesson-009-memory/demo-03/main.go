package main

import "fmt"

func zero(n int) { // n 只是调用方那份数据的副本
	n = 0
}

func main() {
	x := 42
	zero(x)
	fmt.Println("传值之后：", x) // 仍是 42
}
