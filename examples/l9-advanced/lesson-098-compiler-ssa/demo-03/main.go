package main

import (
	"fmt"
	"time"
)

// 观察三：SSA 阶段做边界检查消除。sum1 的 i 由 range 约束，
// 可证明不越界；sum2 每次要额外判断 j 的范围。
func sum1(s []int) int {
	total := 0
	for i := range s {
		total += s[i]
	}
	return total
}

func sum2(s []int, idx []int) int {
	total := 0
	for _, j := range idx {
		if j >= 0 && j < len(s) {
			total += s[j]
		}
	}
	return total
}

func bench(f func()) time.Duration {
	start := time.Now()
	for i := 0; i < 2000; i++ {
		f()
	}
	return time.Since(start)
}

func main() {
	s := make([]int, 1024)
	for i := range s {
		s[i] = i
	}
	idx := make([]int, 1024)
	for i := range idx {
		idx[i] = i % 1024
	}
	fmt.Println("sum1 =", sum1(s))
	fmt.Println("sum2 =", sum2(s, idx))
	fmt.Println("sum1 耗时 =", bench(func() { _ = sum1(s) }))
	fmt.Println("sum2 耗时 =", bench(func() { _ = sum2(s, idx) }))
}
