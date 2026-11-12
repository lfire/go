package main

import "fmt"

// 对应 runtime/mcentral.go 的 partialSwept / partialUnswept
func swept(sg uint32) int   { return int(sg / 2 % 2) }
func unswept(sg uint32) int { return int(1 - sg/2%2) }

func main() {
	for cycle := 0; cycle < 5; cycle++ {
		sg := uint32(2 * cycle) // 每轮 GC，mheap_.sweepgen 加 2
		fmt.Printf("cycle=%d sweepgen=%2d 已清扫→partial[%d] 待清扫→partial[%d]\n",
			cycle, sg, swept(sg), unswept(sg))
	}
}
