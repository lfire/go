package main

import (
	"bufio"
	"fmt"
	"strings"
	"sync"
)

type Event struct {
	TS     int64
	Level  string
	Status int
	Bytes  int
}

func parse(line string) (Event, bool) {
	var e Event
	n, err := fmt.Sscanf(line, "%d %s %d %d", &e.TS, &e.Level, &e.Status, &e.Bytes)
	return e, err == nil && n == 4
}

// Run 把「读取 → 有界 channel → 多 worker 局部聚合 → 扇入 → 按窗口汇总」串成可运行主流程。
func Run(src string, workers, capacity int) map[int64]int64 {
	jobs := make(chan Event, capacity)
	partials := make(chan map[int64]int64, workers)
	go func() {
		defer close(jobs)
		sc := bufio.NewScanner(strings.NewReader(src))
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			if e, ok := parse(sc.Text()); ok {
				jobs <- e
			}
		}
	}()
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make(map[int64]int64)
			for e := range jobs {
				start := e.TS - e.TS%60
				local[start] += int64(e.Bytes)
			}
			partials <- local
		}()
	}
	wg.Wait()
	close(partials)
	total := make(map[int64]int64)
	for p := range partials {
		for k, v := range p {
			total[k] += v
		}
	}
	return total
}

func main() {
	src := "1700000000 INFO 200 1024\n1700000030 WARN 500 64\n1700000061 INFO 200 2048\n"
	fmt.Println("各窗口字节汇总:", Run(src, 3, 4))
}
