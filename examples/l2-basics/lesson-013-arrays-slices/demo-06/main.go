package main

import (
	"fmt"
	"unsafe"
)

func add(s []int) []int { return append(s, 9) }

func main() {
	fmt.Println("描述符字节 =", unsafe.Sizeof([]int(nil)))

	a := make([]int, 3)
	b := make([]int, 0, 3)
	fmt.Println("make(3) 追加后 =", append(a, 9))
	fmt.Println("make(0,3) 追加后 =", append(b, 9))

	s := []int{1, 2}
	_ = add(s)
	fmt.Println("没接住返回值 =", s, len(s))
	s = add(s)
	fmt.Println("接住返回值 =", s, len(s))

	base := []int{1, 2, 3, 4}
	isolated := append(base[0:2:2], 99)
	fmt.Println("压低 cap 后原切片 =", base, "新切片 =", isolated)
}
