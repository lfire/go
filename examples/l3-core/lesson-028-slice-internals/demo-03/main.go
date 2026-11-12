package main

import "fmt"

func main() {
	var s []int
	prev := 0
	for i := 1; i <= 600; i++ {
		s = append(s, i)
		if c := cap(s); c != prev {
			if prev == 0 {
				fmt.Printf("len=%-4d cap=%-4d\n", len(s), c)
			} else {
				fmt.Printf("len=%-4d cap=%-4d 约 %.2f 倍\n", len(s), c, float64(c)/float64(prev))
			}
			prev = c
		}
	}
}
