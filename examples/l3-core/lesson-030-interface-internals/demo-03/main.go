package main

import (
	"fmt"
	"testing"
)

type Adder interface{ Add(int, int) int }
type Impl struct{}

//go:noinline
func (Impl) Add(a, b int) int { return a + b }

//go:noinline
func direct(x Impl, n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += x.Add(i, 1)
	}
	return s
}

//go:noinline
func viaIface(x Adder, n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += x.Add(i, 1)
	}
	return s
}

func main() {
	var impl Impl
	d := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = direct(impl, 100)
		}
	})
	v := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = viaIface(impl, 100)
		}
	})
	fmt.Println("直接调用:", d.String())
	fmt.Println("接口调用:", v.String())
}
