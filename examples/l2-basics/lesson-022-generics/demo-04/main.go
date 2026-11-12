package main

import (
	"fmt"
	"slices"
)

type Number interface{ ~int | ~float64 }

func Sum[T Number](s []T) T {
	var total T
	for _, v := range s {
		total += v
	}
	return total
}

func main() {
	fmt.Println("Sum int =", Sum([]int{1, 2, 3}))
	fmt.Println("Sum float =", Sum([]float64{1.5, 2.5}))

	type Score int
	fmt.Println("Sum 自定义 =", Sum([]Score{1, 2}))

	src := []string{"a", "b"}
	dst := slices.Clone(src)
	dst[0] = "x"
	fmt.Println("Clone 后 =", src, dst)
}
