package main

import "fmt"

func main() {
	var count int = 3 // 显式写出类型：count 只能是 int
	name := "Go"      // 不写类型，编译器由右值推断出 string
	ok := true        // 推断出 bool
	fmt.Printf("%T %T %T\n", count, name, ok)
}
