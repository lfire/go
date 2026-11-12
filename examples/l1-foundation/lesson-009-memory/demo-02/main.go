package main

import "fmt"

func main() {
	x := 42
	p := &x
	q := p
	fmt.Println("两个指针相等吗：", p == q)
	*q = 9
	fmt.Println("通过 q 改之后 x：", x)
}
