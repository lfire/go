package main

import (
	"fmt"
	"testing"
)

var sinkInt int

//go:noinline
func use(s string) int { return len(s) }

//go:noinline
func callUse(b []byte) int { return use(string(b)) }

func main() {
	for _, n := range []int{8, 16, 31, 32, 33, 64} {
		b := make([]byte, n)
		for i := range b {
			b[i] = 'a'
		}
		a := testing.AllocsPerRun(1000, func() { sinkInt = callUse(b) })
		fmt.Printf("string(b) 长度 %2d B: %.0f allocs/op\n", n, a)
	}
}
