package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

var sink int // 写到包级变量，编译器不会把循环优化掉

func main() {
	runtime.GOMAXPROCS(1) // 只给一个 P，最容易暴露「独占」问题
	start := time.Now()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// 没有函数调用、没有 channel、没有 sleep 的紧密循环
		// 协作式抢占在这里找不到安全点
		sum := 0
		for i := 0; i < 500_000_000; i++ {
			sum += i
		}
		sink = sum
	}()

	time.Sleep(10 * time.Millisecond)
	fmt.Printf("主 goroutine 在 %v 时就被调度到了\n", time.Since(start).Round(time.Millisecond))
	wg.Wait()
	fmt.Printf("全部结束，共 %v\n", time.Since(start).Round(time.Millisecond))
}
