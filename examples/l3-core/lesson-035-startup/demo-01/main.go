package main

import "fmt"

var a = setA() // 包级变量先于任何 init 求值
var b = setB() // 依赖 a，故排在 a 之后

func setA() int {
	fmt.Println("1) 包级变量 a 初始化")
	return 1
}
func setB() int {
	fmt.Println("2) 包级变量 b 初始化（依赖 a）")
	return a + 10
}

func init() { fmt.Println("3) 第一个 init，此时 b =", b) }
func init() { fmt.Println("4) 第二个 init") }

func main() {
	fmt.Println("5) main.main 开始，a =", a, "b =", b)
}
