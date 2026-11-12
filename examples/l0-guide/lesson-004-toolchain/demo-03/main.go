package main

import "fmt"

func add(a int, b int) int { // 一个值得被单元测试覆盖的小函数
	return a + b
}

func main() {
	fmt.Printf("40 加 2 等于 %d\n", add(40, 2)) // 格式串与参数类型一致
}
