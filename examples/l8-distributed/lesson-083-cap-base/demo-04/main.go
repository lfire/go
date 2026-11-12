package main

import "fmt"

// combos 返回 n 个副本里所有大小为 k 的子集，用位掩码表示
func combos(n, k int) []int {
	var out []int
	for mask := 0; mask < 1<<n; mask++ {
		if popcount(mask) == k {
			out = append(out, mask)
		}
	}
	return out
}

// popcount 统计位掩码中置位的个数
func popcount(x int) int {
	c := 0
	for x != 0 {
		x &= x - 1
		c++
	}
	return c
}

// intersects 验证任意读集合与写集合是否必然相交
func intersects(n, r, w int) bool {
	reads := combos(n, r)
	writes := combos(n, w)
	for _, rm := range reads {
		for _, wm := range writes {
			if rm&wm == 0 {
				return false
			}
		}
	}
	return true
}

// disjointWrites 判断是否存在两个互不相交的写集合
func disjointWrites(n, w int) bool {
	writes := combos(n, w)
	for _, a := range writes {
		for _, b := range writes {
			if a&b == 0 {
				return true
			}
		}
	}
	return false
}

func main() {
	for _, c := range [][3]int{{3, 2, 2}, {3, 1, 3}, {5, 3, 3}} {
		n, w, r := c[0], c[1], c[2]
		fmt.Printf("N=%d W=%d R=%d 读集合必含已确认副本=%v 存在不相交双写=%v\n",
			n, w, r, intersects(n, r, w), disjointWrites(n, w))
	}
}
