package main

import "fmt"

// Number 是自定义约束：底层类型为 int 或 float64 的类型都算满足。
type Number interface {
	~int | ~float64
}

// Sum 对任意满足 Number 的切片求和。
func Sum[T Number](s []T) T {
	var total T // 零值作起点：int 得 0，float64 得 0
	for _, v := range s {
		total += v // 约束保证了加法对这些类型都合法
	}
	return total
}

func main() {
	ints := []int{1, 2, 3}
	floats := []float64{1.5, 2.5}
	fmt.Println(Sum(ints))      // 6：类型推断出 T 为 int
	fmt.Println(Sum(floats))    // 4：T 为 float64
	fmt.Println(Sum[int](ints)) // 6：也可显式写出类型实参
}
