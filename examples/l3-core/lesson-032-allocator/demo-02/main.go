package main

import (
	"fmt"
	"runtime"
)

type tiny struct{ b byte } // 1 字节、无指针，典型的 tiny 分配候选

var keep *tiny

func main() {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	const N = 100000
	for i := 0; i < N; i++ {
		keep = &tiny{b: byte(i)} // 地址存入全局变量，触发堆分配
	}

	runtime.ReadMemStats(&after)
	fmt.Printf("分配 %d 次，Mallocs 增量 %d，HeapAlloc 增量 %d B\n",
		N, after.Mallocs-before.Mallocs, after.HeapAlloc-before.HeapAlloc)
	fmt.Printf("平均每次约 %.2f B\n", float64(after.HeapAlloc-before.HeapAlloc)/N)
}
