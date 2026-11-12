package main

import "fmt"

func main() {
	base := []int{1, 2, 3, 4}
	left := base[0:2]
	left = append(left, 99) // 就地写入 base[2]
	fmt.Println("append 后 base =", base)

	buf := make([]int, len(base))
	n := copy(buf, base) // 复制到独立数组
	buf[0] = 7
	fmt.Println("copy", n, "个；buf =", buf, "base =", base)

	full := []int{3, 4}
	grown := append(full, 88) // 容量满，分配新数组
	grown[0] = 30
	fmt.Println("full =", full, "grown =", grown)
}
