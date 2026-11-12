package main

import "fmt"

var total = 100 // 包级变量，首字母小写，只在本包可见

func main() {
	fmt.Println("外层：", total)
	{
		total := 5 // 块内短声明，遮蔽了外层的同名变量
		fmt.Println("块内：", total)
	}
	fmt.Println("回到外层：", total)
}
