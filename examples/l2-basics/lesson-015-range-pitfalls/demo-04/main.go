package main

import (
	"fmt"
	"sync"
)

func main() {
	nums := []int{1, 2, 3}
	var wg sync.WaitGroup
	out := make([]int, len(nums))
	for i, v := range nums {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Go 1.22 起 i、v 每轮新建，各 goroutine 互不干扰；
			// 1.21 及更早共用一个变量，会读到最后一轮且构成数据竞争
			out[i] = v * 10
		}()
	}
	wg.Wait()
	fmt.Println("结果 =", out)
}
