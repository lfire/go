package main

import (
	"fmt"
	"runtime"
)

var big []byte

func main() {
	const size = 1 << 20 // 1 MiB，超过约 32 KB 的大对象直达 mheap
	big = make([]byte, size)
	big[0], big[len(big)-1] = 1, 1 // 首尾都写，确保真正占用

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("HeapAlloc=%d MiB, HeapSys=%d MiB\n", m.HeapAlloc>>20, m.HeapSys>>20)
	_ = big
}
