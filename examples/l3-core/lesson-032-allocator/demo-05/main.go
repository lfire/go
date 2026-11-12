package main

import (
	"fmt"
	"runtime"
)

var hold [][]byte

func classOf(n, count int) uint64 {
	runtime.GC()
	var b0, b1 runtime.MemStats
	runtime.ReadMemStats(&b0)
	buf := make([][]byte, count)
	for i := range buf {
		buf[i] = make([]byte, n)
	}
	runtime.ReadMemStats(&b1)
	for i := range b0.BySize {
		if b1.BySize[i].Mallocs-b0.BySize[i].Mallocs >= uint64(count) {
			hold = buf // 保持存活，避免被编译器消除或被 GC 干扰
			return uint64(b0.BySize[i].Size)
		}
	}
	hold = buf
	return 0
}

func main() {
	for _, n := range []int{1, 8, 9, 16, 17, 24, 25, 32, 33, 48, 49, 64} {
		cls := classOf(n, 1000)
		if cls == 0 {
			fmt.Printf("请求 %2d B: BySize 找不到对应档位（tiny 合并）\n", n)
			continue
		}
		fmt.Printf("请求 %2d B → 档位 %2d B，内部碎片 %5.1f%%\n",
			n, cls, 100*float64(int(cls)-n)/float64(cls))
	}
}
