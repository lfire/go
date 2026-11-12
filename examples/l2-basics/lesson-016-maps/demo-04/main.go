package main

import "fmt"

func main() {
	m := map[string]int{"a": 1, "b": 2}
	clear(m) // Go 1.21 起的内置函数，一行清空所有键
	fmt.Println("清空后 len =", len(m))
	m["c"] = 3 // 清空后依然可写
	fmt.Println("重新写入后 len =", len(m))

	var nilMap map[string]int
	clear(nilMap) // 对 nil map 是空操作，不会 panic
	fmt.Println("nil map 清空后 len =", len(nilMap))
}
