package main

import "fmt"

func main() {
	var a [3]int = [3]int{1, 2, 3} // 长度写在类型里
	b := a                          // 整份拷贝
	b[0] = 99
	fmt.Println("a =", a, "b =", b)
	mutate(a)
	fmt.Println("mutate 后 a =", a)
}

func mutate(x [3]int) {
	x[1] = 77 // 只改副本
	fmt.Println("mutate 内 x =", x)
}
