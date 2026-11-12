package main

import "fmt"

func source() []int {
	fmt.Println("source 只调用一次")
	return []int{1, 2, 3}
}

func main() {
	nums := []int{1, 2, 3}
	for _, v := range nums { // v 是副本
		v *= 10
	}
	fmt.Println("遍历后 nums =", nums)

	for i, v := range nums { // 用下标回写
		nums[i] = v * 10
	}
	fmt.Println("按下标改后 nums =", nums)

	for _, v := range source() {
		fmt.Print(v, " ")
	}
	fmt.Println()
}
