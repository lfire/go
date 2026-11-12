package main

import "fmt"

// amplified 计算多层重试链最坏情况下的放大倍数。
// depth 是调用层数，retries 是每层重试次数。
func amplified(depth, retries int) int {
	if depth == 1 {
		return retries + 1
	}
	return (retries + 1) * amplified(depth-1, retries)
}

func main() {
	for _, depth := range []int{1, 2, 3, 4} {
		fmt.Printf("调用层数 %d、每层重试 2 次 -> 最坏下游调用 %d 次\n", depth, amplified(depth, 2))
	}
	fmt.Println("下游完全故障时，重试不会带来成功，只会放大压力")
	fmt.Println("三层调用链无预算时放大到:", amplified(3, 2), "倍")
}
