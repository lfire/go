package main

import (
	"fmt"
	"runtime"
	"unsafe"
)

type node struct{ Val int64 }

// 危险示范：把指针降级成整数后暂存，再跨过可能触发 GC 的调用。
// 结果未定义：对象可能已被回收。仅供教学，切勿在生产使用。
func broken() {
	n := &node{Val: 123}
	addr := uintptr(unsafe.Pointer(n)) // 错误一：GC 不再把 n 视为根

	runtime.GC()           // 错误二：中间发生 GC
	_ = make([]byte, 1024) // 错误三：又一次分配、可能的栈增长

	p := (*node)(unsafe.Pointer(addr)) // 错误四：从陈旧整数还原指针
	fmt.Println("危险读值:", p.Val)      // 可能是垃圾值，甚至崩溃
}

func main() { broken() }
