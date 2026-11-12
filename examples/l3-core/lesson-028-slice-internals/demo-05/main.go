package main

import "fmt"

func main() {
	for _, base := range []int{0, 4, 8, 100, 600} {
		s := make([]int, base, base)
		extra := make([]int, 1000)
		r := append(s, extra...)
		fmt.Printf("oldcap=%4d 一次 append 1000 个 → newcap=%4d\n", base, cap(r))
	}
}
