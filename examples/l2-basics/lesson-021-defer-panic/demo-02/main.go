package main

import "fmt"

// 匿名返回值：return 先把结果拷贝走，defer 改的是另一个局部变量。
func plain() int {
	n := 1
	defer func() { n++ }()
	return n
}

// 命名返回值：return 把值写进具名变量 n，defer 再把它加一。
func named() (n int) {
	n = 1
	defer func() { n++ }()
	return n
}

func main() {
	fmt.Println(plain()) // 1
	fmt.Println(named()) // 2
}
