package main

import (
	"fmt"
	"strconv"
)

func main() {
	var small int8 = 127 // int8 的上界
	small++              // 运行时回绕到 -128，编译器不会拦
	fmt.Println("int8 回绕：", small)
	fmt.Println("本机 int 位数：", strconv.IntSize)
}
