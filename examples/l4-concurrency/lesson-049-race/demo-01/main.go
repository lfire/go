package main

import (
	"fmt"
	"sync"
)

func main() {
	var counter int
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter++ // 数据竞争：读改写不是原子
		}()
	}
	wg.Wait()
	fmt.Println("期望 1000，实际：", counter) // 常小于 1000
}
