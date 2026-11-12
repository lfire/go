package main

import (
	"fmt"
	"unsafe"
)

type A struct {
	Flag  bool
	Count int64
	Tail  bool
}

type B struct {
	Count int64
	Flag  bool
	Tail  bool
}

func main() {
	fmt.Println("A size =", unsafe.Sizeof(A{}), "align =", unsafe.Alignof(A{}))
	fmt.Println("B size =", unsafe.Sizeof(B{}), "align =", unsafe.Alignof(B{}))
	fmt.Println("空结构体 size =", unsafe.Sizeof(struct{}{}))

	s1, s2 := struct{}{}, struct{}{}
	fmt.Println("零大小值相等 =", s1 == s2)
}
