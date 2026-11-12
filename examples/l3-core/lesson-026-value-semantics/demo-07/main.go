package main

import (
	"fmt"
	"time"
)

type Big struct{ data [1024]int }

//go:noinline
func consume(b Big) int { return b.data[0] + b.data[1023] }

//go:noinline
func consumePtr(b *Big) int { return b.data[0] + b.data[1023] }

func main() {
	var big Big
	const N = 5_000_000
	var sink int
	start := time.Now()
	for i := 0; i < N; i++ {
		sink += consume(big)
	}
	dv := time.Since(start)
	start = time.Now()
	for i := 0; i < N; i++ {
		sink += consumePtr(&big)
	}
	dp := time.Since(start)
	fmt.Printf("传值 %v，传指针 %v，sink=%d\n", dv, dp, sink)
}
