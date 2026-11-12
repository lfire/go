package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

func main() {
	runtime.GOMAXPROCS(1) // 全程只有 1 个 P

	var wg sync.WaitGroup
	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			time.Sleep(20 * time.Millisecond) // 模拟阻塞等待：G 挂起，P 被让出
			fmt.Printf("任务 %d 在唯一的一个 P 上被轮流执行\n", id)
		}(i)
	}
	wg.Wait()
	fmt.Println("5 个 G 全部完成，而 P 始终只有 1 个")
}
