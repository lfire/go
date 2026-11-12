package main

import (
	"fmt"
	"slices"
)

func main() {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	// range 顺序不保证，先取键排序
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fmt.Printf("%s=%d ", k, m[k])
	}
	fmt.Println()

	// 只关心次数时可以不接循环变量
	cnt := 0
	for range m {
		cnt++
	}
	fmt.Println("键个数 =", cnt)

	// Set：空结构体作值，零额外开销
	seen := map[string]struct{}{}
	for _, s := range []string{"go", "java", "go", "rust"} {
		seen[s] = struct{}{}
	}
	_, ok := seen["go"]
	fmt.Println("go 存在 =", ok, "去重个数 =", len(seen))
}
