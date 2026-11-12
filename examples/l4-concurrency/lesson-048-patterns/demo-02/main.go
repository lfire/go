package main

import (
	"fmt"
	"sync"
)

// worker 从 jobs 取任务，结果写进自己独占的 out
func worker(id int, jobs <-chan int, out chan<- int) {
	defer close(out) // 独占输出，结束时关闭
	for j := range jobs {
		out <- j * j
		_ = id
	}
}

// merge 扇入：把多个只读通道汇成一个
func merge(cs ...<-chan int) <-chan int {
	var wg sync.WaitGroup
	merged := make(chan int)
	for _, c := range cs {
		wg.Add(1)
		go func(ch <-chan int) {
			defer wg.Done()
			for n := range ch {
				merged <- n
			}
		}(c)
	}
	go func() {
		wg.Wait()
		close(merged) // 所有输入关闭后才关汇合通道
	}()
	return merged
}

func main() {
	jobs := make(chan int)
	go func() {
		defer close(jobs)
		for j := 1; j <= 8; j++ {
			jobs <- j
		}
	}()

	const workers = 4 // 固定数量即 pool 容量
	outs := make([]<-chan int, 0, workers)
	for i := 0; i < workers; i++ {
		out := make(chan int)
		outs = append(outs, out) // 双向可赋给只读
		go worker(i, jobs, out)
	}
	sum := 0
	for n := range merge(outs...) {
		sum += n
	}
	fmt.Println("平方和 =", sum)
}
