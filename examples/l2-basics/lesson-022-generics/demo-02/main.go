package main

import "fmt"

// Index 用 comparable 约束，保证相等比较合法。
func Index[T comparable](s []T, target T) int {
	for i, v := range s {
		if v == target {
			return i
		}
	}
	return -1
}

// Map 有 T 与 U 两个类型参数，把 []T 映射成 []U。
func Map[T, U any](s []T, f func(T) U) []U {
	out := make([]U, 0, len(s))
	for _, v := range s {
		out = append(out, f(v))
	}
	return out
}

// Pair 是泛型类型：字段类型由类型参数决定。
type Pair[K comparable, V any] struct {
	Key   K
	Value V
}

func main() {
	fmt.Println(Index([]string{"go", "rust", "zig"}, "zig")) // 2
	words := []string{"a", "bb", "ccc"}
	fmt.Println(Map(words, func(s string) int { return len(s) })) // [1 2 3]
	p := Pair[string, int]{Key: "age", Value: 18}
	fmt.Println(p.Key, p.Value) // age 18
}
