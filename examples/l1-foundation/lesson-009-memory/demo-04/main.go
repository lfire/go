package main

import "fmt"

func reset(p *int) { // 收到的是一个地址
	*p = 0 // 顺着地址改到原变量
}

func main() {
	x := 42
	p := &x // 取 x 的地址
	fmt.Println("地址指向的值：", *p)
	reset(p)
	fmt.Println("改完之后：", x)
	fmt.Println("p 非空吗：", p != nil)
}
