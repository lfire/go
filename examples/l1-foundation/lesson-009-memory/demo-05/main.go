package main

import "fmt"

func counter() *int { // 返回局部变量的地址，在 Go 里是安全的
	n := 0
	n++
	return &n // 编译器会让 n 逃逸到堆，返回后依然有效
}

func main() {
	p := counter()
	fmt.Println("计数：", *p)
	*p = 9
	fmt.Println("改后：", *p)
	var q *int = nil // 指针的零值是 nil
	fmt.Println("q 是 nil 吗：", q == nil)
}
