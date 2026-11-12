package main

import (
	"fmt"
	"unsafe"
)

// stringHeader 与运行时 string 头一致：数据指针 + 字节长度
type stringHeader struct {
	Data unsafe.Pointer
	Len  int
}

func main() {
	s := "hello, 世界"
	h := (*stringHeader)(unsafe.Pointer(&s))
	fmt.Println("string 头字节数:", unsafe.Sizeof(s)) // 16
	fmt.Println("字节长度 len(s):", len(s))           // 字节数，不是字符数
	fmt.Println("头里的 Len:", h.Len)                 // 与 len(s) 相同
	fmt.Printf("Data 指向: %p, StringData: %p\n", h.Data, unsafe.StringData(s))
	// s[0] = 'H' // 取消注释会编译失败：string 不可变
}
