package main

import "fmt"

func main() {
	var count int          // 只声明，拿到零值 0
	var name string        // 零值是空字符串
	price := 12            // 短声明加推断，等价于 var price int = 12
	fmt.Println(count, name == "", price)
	fmt.Printf("%T %T %T\n", count, name, price)
}
