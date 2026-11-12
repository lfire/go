package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// 观察一：读取 GC 前后的堆与 NextGC 目标，验证调步算法算出的触发点。
func main() {
	old := debug.SetGCPercent(100)
	fmt.Println("原 GOGC =", old)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Println("GC 次数 =", m.NumGC)
	fmt.Println("当前堆分配 =", m.HeapAlloc)
	fmt.Println("下次触发目标 =", m.NextGC)

	const blocks = 200
	hold := make([][]byte, 0, blocks)
	for i := 0; i < blocks; i++ {
		hold = append(hold, make([]byte, 1<<20)) // 每块 1 MiB
	}
	runtime.KeepAlive(hold)

	runtime.ReadMemStats(&m)
	fmt.Println("分配后 GC 次数 =", m.NumGC)
	fmt.Println("分配后堆 =", m.HeapAlloc)
	fmt.Println("新的下次目标 =", m.NextGC)
}
