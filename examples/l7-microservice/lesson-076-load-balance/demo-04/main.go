package main

import "fmt"

func main() {
	const K, N = 10000, 3
	add := N + 1
	modMoved := 0
	for k := 0; k < K; k++ {
		if k%N != k%add {
			modMoved++
		}
	}
	ringMoved := K / add
	fmt.Printf("取模分片迁移: %d/%d（约 %.0f%%）\n", modMoved, K, float64(modMoved)*100/K)
	fmt.Printf("一致性哈希迁移: %d/%d（约 1/%d）\n", ringMoved, K, add)
}
