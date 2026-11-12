package main

import (
	"fmt"
	"slices"
)

func main() {
	type User struct{ Score int }
	users := []*User{{1}, {2}, {3}}
	for _, u := range users {
		u.Score += 10 // 副本仍指向同一对象
	}
	for _, u := range users {
		fmt.Print(u.Score, " ")
	}
	fmt.Println()

	// 要稳定顺序：取键、排序、再按序访问
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys) // Go 1.21 起提供
	for _, k := range keys {
		fmt.Printf("%s=%d ", k, m[k])
	}
	fmt.Println()

	for k := range m { // 遍历中删除是安全的
		if k == "b" {
			delete(m, k)
		}
	}
	fmt.Println("删除后 len =", len(m))
}
