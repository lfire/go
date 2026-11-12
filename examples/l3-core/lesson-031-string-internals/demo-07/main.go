package main

import (
	"fmt"
	"unsafe"
)

// bytesToString 零拷贝：复用 b 的底层数组，不分配
func bytesToString(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// stringToBytes 零拷贝：复用 s 的字节，返回的切片不可写
func stringToBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func main() {
	b := []byte("zero-copy")
	s := bytesToString(b) // s 与 b 共享内存
	fmt.Println("转换结果:", s)
	b[0] = 'Z' // 写 b 会「穿透」到 s
	fmt.Println("改写 b 后 s:", s) // 违反不可变，输出 Z 开头

	ro := stringToBytes("read-only")
	fmt.Println("只读切片:", string(ro))
	// ro[0] = 'X' // 运行时可能崩溃：字符串常在只读段
}
