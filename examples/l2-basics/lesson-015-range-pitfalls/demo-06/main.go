package main

import "fmt"

type Item struct{ N int }

func main() {
	items := []Item{{1}, {2}, {3}}
	for _, v := range items {
		v.N *= 10
	}
	fmt.Println("改副本后 =", items)
	for i := range items {
		items[i].N *= 10
	}
	fmt.Println("下标回写后 =", items)

	nums := []int{1, 2, 3}
	visited := 0
	for _, n := range nums {
		visited++
		if n == 1 {
			nums = append(nums, 4)
		}
	}
	fmt.Println("遍历轮数 =", visited, "追加后 len =", len(nums))

	calls := 0
	source := func() []int {
		calls++
		return []int{1, 2}
	}
	for range source() {
	}
	fmt.Println("source 调用次数 =", calls)
}
