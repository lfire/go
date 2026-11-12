package main

import "fmt"

func main() {
	n := 65
	fmt.Println(string(n)) // 能编译，但 vet 会警告：把整数当码点
}
