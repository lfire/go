package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func worker(ctx context.Context, name string, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			fmt.Println(name, "收到取消：", ctx.Err())
			return
		default:
			time.Sleep(10 * time.Millisecond) // 模拟工作
		}
	}
}

func main() {
	root, cancel := context.WithCancel(context.Background())
	childA, cancelA := context.WithCancel(root)
	defer cancelA()
	childB, cancelB := context.WithTimeout(root, 5*time.Second)
	defer cancelB()

	var wg sync.WaitGroup
	wg.Add(2)
	go worker(childA, "worker-A", &wg)
	go worker(childB, "worker-B", &wg)
	time.Sleep(50 * time.Millisecond)
	cancel() // 只取消 root，两个子同时收到
	wg.Wait()
	fmt.Println("全部 worker 已退出")
}
