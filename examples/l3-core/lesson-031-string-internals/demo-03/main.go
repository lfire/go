package main

import (
	"fmt"
	"unsafe"
)

func main() {
	s := "immutable"
	b := []byte(s) // 转换：分配新数组并拷贝
	fmt.Printf("s Data=%p, b Data=%p\n", unsafe.StringData(s), unsafe.SliceData(b))
	b[0] = 'I' // 只改副本
	fmt.Println("s 仍是:", s)
	fmt.Println("b 变成:", string(b)) // 又一次拷贝

	s2 := string(b) // []byte 转 string 同样拷贝
	b[0] = 'X'
	fmt.Println("s2 不受影响:", s2)
}
