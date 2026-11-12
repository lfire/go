package main

import "fmt"

func counter() func() int { // 返回值本身是一个函数
	n := 0
	return func() int { // 匿名函数捕获了外层的 n
		n++
		return n
	}
}

func main() {
	next := counter()
	fmt.Println(next(), next(), next())
	defer fmt.Println("这句最后才执行")
	fmt.Println("正常流程")
}
