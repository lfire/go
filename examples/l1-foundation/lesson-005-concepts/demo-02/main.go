package main

import "fmt"

func main() {
	// 同一份数据，一次声明、反复使用
	a, b := 12, 3
	// 处理一：相加
	fmt.Println("相加：", a+b)
	// 处理二：相乘
	fmt.Println("相乘：", a*b)
	// 处理三：取较大者
	fmt.Println("较大者：", max(a, b))
}
