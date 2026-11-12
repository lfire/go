package main

/*
#include <stdint.h>
static int32_t c_noop(void) { return 0; }
static int32_t c_add(int32_t a, int32_t b) { return a + b; }
static int32_t c_sum(const int32_t *p, int32_t n) {
	int32_t s = 0;
	for (int32_t i = 0; i < n; i++) s += p[i];
	return s;
}
*/
import "C"

import (
	"fmt"
	"testing"
)

var sink int32
var buf [1024]int32

func main() {
	for i := range buf {
		buf[i] = int32(i)
	}
	rNoop := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sink = int32(C.c_noop())
		}
	})
	rAdd := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sink = int32(C.c_add(1, 2))
		}
	})
	rBatch := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sink = int32(C.c_sum((*C.int32_t)(&buf[0]), 1024))
		}
	})
	fmt.Println("noop :", rNoop.String())
	fmt.Println("add  :", rAdd.String())
	fmt.Println("batch:", rBatch.String())
	fmt.Printf("batch 每元素 ns: %.4f\n",
		float64(rBatch.T.Nanoseconds())/float64(rBatch.N)/1024)
}
