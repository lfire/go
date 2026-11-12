package main

import (
	"fmt"
	"testing"
)

func noHint(n int) map[int]int {
	m := map[int]int{} // 无 hint，插入过程多次翻倍扩容
	for i := 0; i < n; i++ {
		m[i] = i
	}
	return m
}

func withHint(n int) map[int]int {
	m := make(map[int]int, n) // 一次给足容量
	for i := 0; i < n; i++ {
		m[i] = i
	}
	return m
}

func main() {
	const n = 10000
	a := testing.AllocsPerRun(20, func() { _ = noHint(n) })
	b := testing.AllocsPerRun(20, func() { _ = withHint(n) })
	fmt.Printf("无 hint 每次分配约 %.0f 次，带 hint 约 %.0f 次\n", a, b)
}
