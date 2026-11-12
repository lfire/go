package main

import (
	"fmt"
	"runtime"
)

func heapKB() uint64 {
	var s runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&s)
	return s.HeapAlloc / 1024
}

func main() {
	m := make(map[int]int)
	for i := 0; i < 200000; i++ {
		m[i] = i
	}
	fmt.Println("满载 len:", len(m), " HeapAlloc KB:", heapKB())
	for i := 0; i < 199000; i++ {
		delete(m, i)
	}
	fmt.Println("删到 1000 项 len:", len(m), " HeapAlloc KB:", heapKB())
}
