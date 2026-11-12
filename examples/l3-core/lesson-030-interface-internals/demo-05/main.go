package main

import (
	"fmt"
	"testing"
)

var sink any

//go:noinline
func boxInt(v int) any { return any(v) }

func main() {
	for _, v := range []int{1, 255, 256, 1000} {
		a := testing.AllocsPerRun(1000, func() { sink = boxInt(v) })
		fmt.Printf("装箱 %4d: %.0f allocs/op\n", v, a)
	}
}
