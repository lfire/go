package main

import (
	"fmt"
	"runtime"
	"sync"
)

func main() {
	runtime.GOMAXPROCS(1) // 只有一个 P，任何时刻至多一个 goroutine 在执行

	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 1; j <= 2; j++ {
				fmt.Printf("goroutine %d 第 %d 步\n", id, j)
				runtime.Gosched() // 主动让出，让同一线程上的其它 goroutine 也跑一步
			}
		}(i)
	}
	wg.Wait()
	fmt.Println("全部完成：单核上它们是并发，但不是并行")
}
