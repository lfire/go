package main

import (
	"fmt"
	"runtime"
)

var sink []*[]byte

func main() {
	for i := 0; i < 20000; i++ {
		b := make([]byte, 64+i%8)
		sink = append(sink, &b)
	}
	n, ok := runtime.MemProfile(nil, true)
	fmt.Printf("MemProfileRate=%d  MemProfile 记录数 n=%d ok=%v\n", runtime.MemProfileRate, n, ok)
}
