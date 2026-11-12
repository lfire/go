package main

import "fmt"

func main() {
	big := make([]int, 1000)
	for i := range big {
		big[i] = i
	}

	part := big[:3:3] // len=3, cap=3，append 必然另分配
	fmt.Println("part len/cap:", len(part), cap(part))
	part = append(part, 999)
	fmt.Println("big[3] 仍是:", big[3]) // 3，未被覆盖

	alias := big[:3] // cap 继承为 1000
	alias = append(alias, 888)
	fmt.Println("big[3] 变成:", big[3], " alias:", alias) // 888，被意外改写

	keep := make([]int, 3) // 想长期只留 3 个元素，用 copy 切断
	copy(keep, big[:3])
	fmt.Println("keep:", keep)
}
