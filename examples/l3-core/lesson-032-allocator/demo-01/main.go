package main

import (
	"fmt"
	"runtime"
)

// 全局变量承接分配结果，避免被编译器判定为死代码而消除
var sink []byte

// allocOfSize 申请 n 字节，返回 HeapAlloc 的近似增量
func allocOfSize(n int) uint64 {
	runtime.GC() // 先回收上一轮残留，减少干扰
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	sink = make([]byte, n) // 逃逸到堆，必然经过分配器
	runtime.ReadMemStats(&after)
	return after.HeapAlloc - before.HeapAlloc
}

func main() {
	for _, n := range []int{1, 8, 16, 17, 32, 33, 64, 1024} {
		fmt.Printf("申请 %5d B，HeapAlloc 增量约 %5d B\n", n, allocOfSize(n))
	}
}
