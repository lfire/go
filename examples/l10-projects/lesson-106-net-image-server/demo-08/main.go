package main

import (
	"fmt"
	"sync"
	"time"
)

// worker 从 jobs 取任务、把结果写入 results；jobs 关闭且排空后退出。
func worker(id int, jobs <-chan int, results chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range jobs {
		time.Sleep(time.Millisecond) // 模拟缩略图处理
		results <- fmt.Sprintf("worker=%d job=%d", id, job)
	}
}

func main() {
	const total, workers = 10, 4
	jobs := make(chan int)              // 无缓冲，天然背压
	results := make(chan string, total) // 有缓冲，避免 worker 被消费者阻塞
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker(i, jobs, results, &wg)
	}
	go func() {
		for i := 0; i < total; i++ {
			jobs <- i
		}
		close(jobs) // 必须关闭，否则 worker 永远阻塞
		wg.Wait()
		close(results) // 所有生产者退出后才关结果通道
	}()
	count := 0
	for range results {
		count++
	}
	fmt.Println("处理结果数:", count, "worker 数:", workers)
}
