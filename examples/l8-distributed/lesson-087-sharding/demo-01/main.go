package main

import "fmt"

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// moveRatio 计算取模分片从 oldN 扩到 newN 时必须迁移的 key 比例。
// 在 lcm 个余数上枚举，归属不变的才算不用搬。
func moveRatio(oldN, newN int) float64 {
	lcm := oldN * newN / gcd(oldN, newN)
	same := 0
	for r := 0; r < lcm; r++ {
		if r%oldN == r%newN {
			same++
		}
	}
	return 1 - float64(same)/float64(lcm)
}

func main() {
	for _, c := range [][2]int{{3, 4}, {4, 5}, {10, 11}} {
		fmt.Printf("取模 %d 扩到 %d 台：迁移比例 %.1f%%\n", c[0], c[1], moveRatio(c[0], c[1])*100)
	}
}
