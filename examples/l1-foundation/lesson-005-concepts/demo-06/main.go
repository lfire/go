package main

import "fmt"

func main() {
	var count int   // 零值 0，不是随机值
	var name string // 零值是空串，不是 nil
	fmt.Println("count：", count, "名字长度：", len(name))
}
