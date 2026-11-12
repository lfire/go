package main

import "fmt"

// 循环上界来自 len(xs)：编译器能证明下标恒合法，
// 从而消除每次迭代的边界检查（BCE）
func sum(xs []int) int {
	total := 0
	for i := 0; i < len(xs); i++ {
		total += xs[i]
	}
	return total
}

func main() {
	fmt.Println(sum([]int{1, 2, 3, 4, 5}))
}
