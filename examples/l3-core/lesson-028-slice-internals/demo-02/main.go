package main

import "fmt"

func main() {
	base := []int{1, 2, 3, 4}
	a := base[1:3] // len=2, cap=3，与 base 共享
	a[0] = 99      // 改动 base 也能看见
	fmt.Println("改 a 后 base:", base)

	b := append(a, 7) // cap 够用，不扩容，写在同一数组
	fmt.Println("append 未扩容 b:", b, " base:", base)

	c := append(a, 8, 9, 10) // 超出 cap，触发扩容，另起炉灶
	c[0] = 777
	fmt.Println("base:", base, " c:", c)
	fmt.Printf("a 底层 %p, c 底层 %p\n", &a[0], &c[0])
}
