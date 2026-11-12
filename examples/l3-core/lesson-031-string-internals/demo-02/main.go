package main

import (
	"fmt"
	"unsafe"
)

func main() {
	s := "Go语言"
	sub := s[:2] // 子串共享同一块底层字节，不拷贝
	fmt.Printf("s   Data=%p\n", unsafe.StringData(s))
	fmt.Printf("sub Data=%p\n", unsafe.StringData(sub)) // 相同地址
	fmt.Println("字节数:", len(s), " rune 数:", len([]rune(s)))
	for i, r := range s {
		fmt.Printf("字节偏移 %d 对应 %c\n", i, r)
	}
}
