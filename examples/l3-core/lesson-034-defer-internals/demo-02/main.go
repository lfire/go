package main

import "fmt"

// 循环内 defer：执行次数编译期不确定，无法用固定位图展开
func loopDefer(n int) {
	for i := 0; i < n; i++ {
		defer fmt.Println("清理", i)
	}
}

// 超过 8 条：deferBits 只有 8 位，放不下
func manyDefer() {
	defer fmt.Println(1)
	defer fmt.Println(2)
	defer fmt.Println(3)
	defer fmt.Println(4)
	defer fmt.Println(5)
	defer fmt.Println(6)
	defer fmt.Println(7)
	defer fmt.Println(8)
	defer fmt.Println(9) // 第 9 条触发退化
}

func main() {
	loopDefer(3)
	manyDefer()
}
