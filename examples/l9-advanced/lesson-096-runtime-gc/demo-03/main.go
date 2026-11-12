package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// 观察三：MemoryLimit 是软上限，与 GOGC 一起约束堆目标。
func main() {
	const limit = 64 << 20 // 64 MiB
	prev := debug.SetMemoryLimit(limit)
	fmt.Println("原内存上限 =", prev)
	fmt.Println("当前内存上限 =", debug.SetMemoryLimit(-1)) // 传 -1 只查询不修改

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Println("初始 GC 次数 =", m.NumGC)
	fmt.Println("下次 GC 目标 =", m.NextGC)

	var keep [][]byte
	for i := 0; i < 100; i++ {
		keep = append(keep, make([]byte, 1<<20))
		if len(keep) > 30 {
			keep = keep[len(keep)-15:]
		}
	}
	runtime.KeepAlive(keep)
	runtime.ReadMemStats(&m)
	fmt.Println("施压后 GC 次数 =", m.NumGC)
	fmt.Println("施压后堆 =", m.HeapAlloc)
}
