package main

import "fmt"

func demo() {
	defer fmt.Println("第 1 个 defer") // 最后执行
	defer fmt.Println("第 2 个 defer")
	defer fmt.Println("第 3 个 defer") // 最先执行

	x := 10
	defer fmt.Println("x 的快照:", x) // 此刻参数就固定为 10
	x = 99
	fmt.Println("函数体结束, x =", x)
}

func main() {
	demo()
}
