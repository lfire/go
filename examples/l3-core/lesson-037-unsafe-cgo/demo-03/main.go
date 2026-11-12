package main

import (
	"fmt"
	"runtime"
	"unsafe"
)

type node struct{ Val int64 }

// 正确一：全程 Pointer，偏移用 unsafe.Add，绝不落地成 uintptr 变量
func byPointer(n *node, off uintptr) int64 {
	return *(*int64)(unsafe.Add(unsafe.Pointer(n), off))
}

// 正确二：确需 uintptr 时（如 syscall），转换在同一表达式内，
// 并用 KeepAlive 把对象保活到最后一次使用之后
func withKeepAlive(n *node) int64 {
	p := (*int64)(unsafe.Pointer(uintptr(unsafe.Pointer(n)) + 0))
	runtime.GC()
	v := *p
	runtime.KeepAlive(n)
	return v
}

func main() {
	n := &node{Val: 123}
	fmt.Println("按偏移读:", byPointer(n, unsafe.Offsetof(n.Val)))
	fmt.Println("保活读值:", withKeepAlive(n))
}
