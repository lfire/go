package main

import (
	"fmt"
	"unsafe"
)

type header struct {
	Flag  byte
	Count int32
	Data  int64
}

func main() {
	h := header{Flag: 1, Count: 7, Data: 42}

	// 规则一：指针转 Pointer 再转另一种指针，要求布局等价
	p := unsafe.Pointer(&h)
	asBytes := (*[unsafe.Sizeof(h)]byte)(p)
	fmt.Printf("结构体大小 = %d 字节，首字节 = %d\n", unsafe.Sizeof(h), asBytes[0])

	// 规则二：字段偏移用 Offsetof，配合 unsafe.Add 在同一表达式内完成，
	// 全程是 Pointer，不产生游离的 uintptr 中间变量
	off := unsafe.Offsetof(h.Data)
	dataPtr := (*int64)(unsafe.Add(p, off)) // Go 1.17+
	fmt.Printf("Data 偏移 = %d，读到 = %d\n", off, *dataPtr)

	*dataPtr = 99
	fmt.Println("改写后 h.Data =", h.Data)
}
