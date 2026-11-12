package main

import "fmt"

// expectedCalls 返回失败概率 p、最多重试 n 次时，单个原始请求的期望总调用次数。
func expectedCalls(p float64, n int) float64 {
	total, term := 0.0, 1.0
	for i := 0; i <= n; i++ {
		total += term
		term *= p
	}
	return total
}

func main() {
	for _, p := range []float64{0.1, 0.5, 1.0} {
		fmt.Printf("p=%.1f 最多重试2次: 期望放大 %.2f 倍\n", p, expectedCalls(p, 2))
	}
}
