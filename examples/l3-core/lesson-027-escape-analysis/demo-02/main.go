package main

import "fmt"

func makeSlice(n int) []byte {
	return make([]byte, n) // 切片要活着离开函数，底层数组在堆上
}

func main() {
	// 1) 被闭包捕获：x 要在函数返回后仍被访问
	x := 42
	f := func() int { return x }
	fmt.Println(f())

	// 2) 装箱进 interface{}：具体类型信息要随值一起存活
	var a any = 42
	fmt.Println(a)

	// 3) 传给 fmt 的 ...any 可变参数
	n := 7
	fmt.Println("n =", n)

	// 4) 动态长度且被返回的切片
	b := makeSlice(n)
	fmt.Println(len(b))
}
