package main

import "fmt"

func main() {
	s1 := []int{10, 20, 30} // len 与 cap 都等于 3
	s2 := make([]int, 3, 8) // 预分配，减少 append 扩容
	fmt.Println("s1:", s1, len(s1), cap(s1))
	fmt.Println("s2:", s2, len(s2), cap(s2))

	arr := [5]int{0, 1, 2, 3, 4}
	part := arr[1:4] // 左闭右开，取下标 1、2、3
	fmt.Println("part:", part, len(part), cap(part))
}
