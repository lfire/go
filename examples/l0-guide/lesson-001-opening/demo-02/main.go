package main

import "fmt"

func main() {
	var year int = 2009 // 显式声明：year 从此只能是 int
	var next = year + 1 // 短声明：类型由右值推断，但推断后依旧固定
	fmt.Println(next)   // 输出 2010
	fmt.Printf("%T\n", year) // %T 打印变量的类型：int
}
