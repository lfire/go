package main

import (
	"fmt"
	"slices"
	"strings"
)

// Stringer 是方法约束：要求类型实现 String()。
type Stringer interface {
	String() string
}

func JoinAll[T Stringer](items []T) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, it.String())
	}
	return strings.Join(parts, ",")
}

// Box 是泛型类型；方法接收者上要重新声明类型参数 T。
type Box[T any] struct {
	Value T
}

func (b Box[T]) Get() T { return b.Value }

// code 的底层类型是 int，且实现了 String()。
type code int

func (c code) String() string { return fmt.Sprintf("CODE-%d", c) }

func main() {
	fmt.Println(JoinAll([]code{7, 8})) // CODE-7,CODE-8
	b := Box[string]{Value: "ok"}
	fmt.Println(b.Get()) // ok

	nums := []int{3, 1, 2}
	slices.Sort(nums)
	fmt.Println(nums)                     // [1 2 3]
	fmt.Println(slices.Contains(nums, 2)) // true
}
