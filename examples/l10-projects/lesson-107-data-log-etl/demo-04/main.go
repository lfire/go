package main

import (
	"fmt"
	"sync"
)

type Event struct {
	TS     int64
	Level  string
	Status int
	Bytes  int
}

// Partial 是单个 worker 的局部聚合结果，消费期间无锁累加。
type Partial struct {
	Count  int64
	Bytes  int64
	Errors int64
}

func (p *Partial) add(e Event) {
	p.Count++
	p.Bytes += int64(e.Bytes)
	if e.Status >= 500 {
		p.Errors++
	}
}

// worker 只累加自己的局部结果，结束时发一次，避免每行争抢同一把锁。
func worker(id int, jobs <-chan Event, results chan<- Partial, wg *sync.WaitGroup) {
	defer wg.Done()
	var p Partial
	for e := range jobs {
		p.add(e)
	}
	_ = id
	results <- p
}

// FanIn 合并所有局部结果。results 缓冲必须不小于 worker 数，
// 否则最后几个 worker 的发送阻塞，而 wg.Wait 又等它们归零，形成自死锁。
func FanIn(jobs <-chan Event, n int) Partial {
	results := make(chan Partial, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go worker(i, jobs, results, &wg)
	}
	wg.Wait()
	close(results)
	total := Partial{}
	for p := range results {
		total.Count += p.Count
		total.Bytes += p.Bytes
		total.Errors += p.Errors
	}
	return total
}

func main() {
	jobs := make(chan Event, 16)
	for _, s := range []int{200, 500, 404, 503, 200} {
		jobs <- Event{Status: s, Bytes: 128}
	}
	close(jobs)
	fmt.Printf("合计: %+v\n", FanIn(jobs, 3))
}
