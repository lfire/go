package main

import (
	"fmt"
	"testing"
)

var sink string

//go:noinline
func convReturn(b []byte) string { return string(b) } // 结果外泄，必须拷贝

//go:noinline
func convKey(m map[string]int, b []byte) int { return m[string(b)] } // 只读临时，可省略

func main() {
	b := []byte("key")
	m := map[string]int{"key": 7}
	a := testing.AllocsPerRun(1000, func() { sink = convReturn(b) })
	c := testing.AllocsPerRun(1000, func() { _ = convKey(m, b) })
	fmt.Printf("返回式约 %.0f allocs/op，临时键约 %.0f allocs/op\n", a, c)
}
