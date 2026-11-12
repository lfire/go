package main

import "fmt"

func main() {
	price := 12            // 数据：单价这个整数
	count := 3             // 数据：数量这个整数
	total := price * count // 处理：把两份数据相乘，得到新数据
	fmt.Println("合计：", total) // 处理：把结果送到屏幕
}
