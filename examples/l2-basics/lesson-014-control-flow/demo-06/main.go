package main

import "fmt"

// find 在二维表里找 target，返回行列与是否命中。
// 用标签 break 一次跳出两层循环，比 goto 更清晰；
// 没命中要靠第三个返回值明确区分，别把 0 行列当成功。
func find(grid [][]int, target int) (int, int, bool) {
	ri, rj := -1, -1
FOUND:
	for i, row := range grid {
		for j, v := range row {
			if v == target {
				ri, rj = i, j
				break FOUND
			}
		}
	}
	if ri == -1 {
		return 0, 0, false
	}
	return ri, rj, true
}

func main() {
	grid := [][]int{{1, 2}, {3, 4}}
	if i, j, ok := find(grid, 4); ok {
		fmt.Println("找到于", i, j)
	}
	if _, _, ok := find(grid, 99); !ok {
		fmt.Println("没找到 99，别把零值下标当成命中")
	}
	if _, _, ok := find(nil, 1); !ok { // nil 表格也安全
		fmt.Println("nil 输入也安全")
	}
}
