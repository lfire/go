package main

import (
	"fmt"
	"slices"
)

func main() {
	// nil 切片
	var nilSlice []int
	// 非 nil 的空切片
	emptySlice := make([]int, 0)

	c1 := slices.Clone(nilSlice)
	c2 := slices.Clone(emptySlice)
	fmt.Println("克隆 nil 后仍是 nil =", c1 == nil)
	fmt.Println("克隆空切片后仍是 nil =", c2 == nil)

	src := []int{1, 2, 3}
	c3 := slices.Clone(src)
	c3[0] = 99 // 改副本，原切片不受影响
	fmt.Println("c3 =", c3, "src =", src)
	fmt.Println("包含 2 =", slices.Contains(src, 2))
}
