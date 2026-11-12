package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// 修复二：原子操作，适合单个数
func byAtomic() int64 {
	var n atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.Add(1)
		}()
	}
	wg.Wait()
	return n.Load()
}

// 修复三：channel 传递所有权，同一时刻只有一个改数据
func byChannel() int {
	ch := make(chan int, 1)
	ch <- 0 // 把所有权放进通道
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := <-ch   // 取到所有权
			ch <- v + 1 // 改完放回
		}()
	}
	wg.Wait()
	return <-ch
}

func main() {
	fmt.Println("atomic 修复：", byAtomic())
	fmt.Println("channel 修复：", byChannel())
}
