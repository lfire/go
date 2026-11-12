package main

import (
	"fmt"
	"runtime"
)

// 包级变量：在 main 之前完成分配，且常驻堆
var table = make([]int, 1<<20) // 约 8 MiB

func init() {
	// init 里做重活会拖慢每个导入本包的程序，包括测试
	for i := range table {
		table[i] = i
	}
}

func main() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("main 一开始 HeapAlloc 已有 %d MiB\n", m.HeapAlloc>>20)
	fmt.Printf("table 长度 = %d，首元素 = %d\n", len(table), table[0])
}
