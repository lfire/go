package main

import (
	"fmt"
	"runtime"
)

var sink [][]byte

func main() {
	for _, n := range []int{8, 17, 100, 4096} {
		sink = append(sink, make([]byte, n))
	}
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	for _, s := range m.BySize {
		if s.Mallocs > 0 {
			fmt.Printf("档位 %5d B: Mallocs=%d Frees=%d\n", s.Size, s.Mallocs, s.Frees)
		}
	}
	_ = sink
}
