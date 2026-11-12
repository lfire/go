package main

import "fmt"

// Route 按固定配额把一定比例的请求分给 canary
func Route(seq, canaryPercent int) string {
	if seq%100 < canaryPercent {
		return "canary"
	}
	return "stable"
}

func main() {
	for _, percent := range []int{5, 20, 50, 100} {
		canary := 0
		for i := 0; i < 1000; i++ {
			if Route(i, percent) == "canary" {
				canary++
			}
		}
		fmt.Printf("金丝雀权重 %3d%% : 命中 canary %d / 1000\n", percent, canary)
	}
}
