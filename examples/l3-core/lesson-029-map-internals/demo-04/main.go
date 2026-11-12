package main

import "fmt"

//go:noinline
func ops(m map[int]int, k, v int) int {
	m[k] = v
	x := m[k]
	delete(m, k)
	n := 0
	for range m {
		n++
	}
	return x + n
}

func main() {
	m := make(map[int]int)
	fmt.Println(ops(m, 1, 2))
}
