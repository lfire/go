package main

import (
	"fmt"
	"sync"
)

// runAll 模拟 errgroup：带并发上限，返回首个错误
func runAll(tasks []int, limit int) error {
	sem := make(chan struct{}, limit) // 信号量即背压
	errCh := make(chan error, len(tasks))
	var wg sync.WaitGroup

	for _, t := range tasks {
		sem <- struct{}{} // 达上限就阻塞，直到有人释放
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			defer func() { <-sem }() // 完成一个释放一个
			if n == 3 {
				errCh <- fmt.Errorf("任务 %d 执行失败", n)
				return
			}
			fmt.Println("完成任务", n)
		}(t)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh { // 只取第一个错误
		return err
	}
	return nil
}

func main() {
	if err := runAll([]int{1, 2, 3, 4, 5}, 2); err != nil {
		fmt.Println("首个错误：", err)
	}
}
