package main

import "fmt"

func main() {
	x := 1
	defer fmt.Println("defer 里的 x：", x)
	x = 9
	fmt.Println("正常流程的 x：", x)
}
