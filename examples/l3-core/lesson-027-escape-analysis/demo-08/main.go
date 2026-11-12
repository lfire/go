package main

import (
	"fmt"
	"testing"
)

//go:noinline
func noPrealloc(n int) []int {
	var s []int // nil slice，append 会多次扩容与堆分配
	for i := 0; i < n; i++ {
		s = append(s, i)
	}
	return s
}

//go:noinline
func prealloc(n int) []int {
	s := make([]int, 0, n) // 一次分配到位
	for i := 0; i < n; i++ {
		s = append(s, i)
	}
	return s
}

func main() {
	const n = 1024
	a := testing.AllocsPerRun(100, func() { _ = noPrealloc(n) })
	b := testing.AllocsPerRun(100, func() { _ = prealloc(n) })
	fmt.Printf("无预分配每次约 %.0f 次分配，预分配约 %.0f 次\n", a, b)
}
