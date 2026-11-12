package main

import "fmt"

type Adder interface{ Add(int, int) int }
type Impl struct{}

//go:noinline
func (Impl) Add(a, b int) int { return a + b }

//go:noinline
func concrete(x Impl, n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += x.Add(i, 1)
	}
	return s
}

//go:noinline
func dynamic(x Adder, n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += x.Add(i, 1)
	}
	return s
}

func main() {
	fmt.Println(concrete(Impl{}, 10), dynamic(Adder(Impl{}), 10))
}
