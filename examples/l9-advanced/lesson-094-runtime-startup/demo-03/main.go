package main

import (
	"fmt"
	"runtime"
)

// 观察三：main 刚执行时，堆分配器与 GC 已被 schedinit / gcenable 初始化。
func main() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Println("堆已向 OS 申请的字节数 =", m.HeapSys)
	fmt.Println("已完成 GC 次数 =", m.NumGC)
	fmt.Println("分配器已初始化 =", m.HeapSys > 0)
}
