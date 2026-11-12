package main

import "fmt"

func main() {
	var p *int
	fmt.Println("p 是 nil 吗：", p == nil)
	fmt.Println(*p)
}
