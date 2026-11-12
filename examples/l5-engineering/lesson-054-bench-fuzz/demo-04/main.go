package main

import (
	"fmt"
	"testing"
)

var sink int

func work(n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += i
	}
	return s
}

func main() {
	r := testing.Benchmark(func(b *testing.B) {
		fmt.Println("本轮 b.N =", b.N)
		for i := 0; i < b.N; i++ {
			sink = work(100)
		}
	})
	fmt.Println("最终 ns/op =", r.NsPerOp(), " 收敛的 N =", r.N)
}
