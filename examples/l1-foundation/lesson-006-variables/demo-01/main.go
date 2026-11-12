package main

import "fmt"

func main() {
	var (
		i int
		f float64
		s string
		b bool
		p *int
	)
	fmt.Println("数值零值：", i, f)
	fmt.Println("字符串是空串吗：", s == "", "布尔零值：", b)
	fmt.Println("指针是 nil 吗：", p == nil)
}
