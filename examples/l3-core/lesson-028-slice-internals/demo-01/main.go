package main

import (
	"fmt"
	"unsafe"
)

// sliceHeader 与运行时布局一致，仅用于教学观察
type sliceHeader struct {
	Data unsafe.Pointer
	Len  int
	Cap  int
}

func main() {
	s := make([]int, 3, 5)
	s[0], s[1], s[2] = 10, 20, 30
	h := (*sliceHeader)(unsafe.Pointer(&s))
	fmt.Println("切片头字节数:", unsafe.Sizeof(s)) // 24
	fmt.Println("头里的 Len / Cap:", h.Len, h.Cap) // 3 / 5
	fmt.Printf("Data 指向: %p\n", h.Data)
	fmt.Println("首元素值:", *(*int)(h.Data)) // 10
}
