package main

import (
	"fmt"
	"unsafe"
)

func main() {
	var m map[string]int
	fmt.Println("map 变量大小(字节):", unsafe.Sizeof(m)) // 8：一个 *hmap
	m = make(map[string]int, 8)
	for i := 0; i < 5; i++ {
		m[fmt.Sprintf("k%d", i)] = i
	}
	// 多次遍历顺序不保证一致：起始桶与起始槽是随机的
	for r := 0; r < 3; r++ {
		for k, v := range m {
			fmt.Printf("%s=%d ", k, v)
		}
		fmt.Println()
	}
	// 下面这行若取消注释会编译失败：cannot take address of m["k0"]
	// _ = &m["k0"]
}
