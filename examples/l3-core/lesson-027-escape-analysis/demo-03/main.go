package main

import (
	"fmt"
	"runtime"
)

//go:noinline
func noAlloc() int {
	x := [64]byte{} // 纯栈上，不逃逸
	return int(x[0])
}

//go:noinline
func withAlloc() *int {
	x := 1
	return &x // 逃逸，每次堆分配
}

func countAllocs(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := 0; i < 1000; i++ {
		f()
	}
	runtime.ReadMemStats(&after)
	return after.Mallocs - before.Mallocs
}

func main() {
	fmt.Println("noAlloc 1000 次 Mallocs:", countAllocs(func() { _ = noAlloc() }))
	fmt.Println("withAlloc 1000 次 Mallocs:", countAllocs(func() { _ = withAlloc() }))
}
