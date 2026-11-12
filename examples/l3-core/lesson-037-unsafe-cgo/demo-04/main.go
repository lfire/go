package main

/*
#include <stdint.h>
static int32_t c_add(int32_t a, int32_t b) { return a + b; }
*/
import "C"

import (
	"fmt"
	"testing"
)

//go:noinline
func goAdd(a, b int32) int32 { return a + b }

var sink int32

func main() {
	g := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sink = goAdd(1, 2)
		}
	})
	c := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sink = int32(C.c_add(1, 2))
		}
	})
	fmt.Printf("纯 Go: %s\n跨 cgo: %s\n", g.String(), c.String())
}
