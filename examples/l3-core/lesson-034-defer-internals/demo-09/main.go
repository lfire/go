package main

import (
	"fmt"
	"testing"
)

func cleanup() {}
func work()    { _ = 1 + 1 }

func withDefer()    { defer cleanup(); work() }
func withoutDefer() { work(); cleanup() }

func main() {
	r1 := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			withDefer()
		}
	})
	r2 := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			withoutDefer()
		}
	})
	fmt.Printf("开放编码 defer: %s\n", r1.String())
	fmt.Printf("手动清理      : %s\n", r2.String())
	fmt.Printf("每次分配字节  : defer=%d, 手动=%d\n",
		r1.AllocedBytesPerOp(), r2.AllocedBytesPerOp())
}
