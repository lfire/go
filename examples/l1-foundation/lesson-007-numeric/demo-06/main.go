package main

import "fmt"

func main() {
	var u uint8 = 0
	f := 3.9
	fmt.Println("uint8 下溢：", u-1)
	fmt.Println("float 转 int 截断：", int(f))
	cents := 1999
	fmt.Println("金额：", cents/100, "元", cents%100, "分")
}
