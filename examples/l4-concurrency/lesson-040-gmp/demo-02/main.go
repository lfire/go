package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

// burn 做一段纯计算，会持续占住一个 P
func burn(wg *sync.WaitGroup) {
	defer wg.Done()
	sum := 0
	for i := 0; i < 50000000; i++ {
		sum += i
	}
	_ = sum
}

func run(n int) time.Duration {
	runtime.GOMAXPROCS(n)
	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(2)
	go burn(&wg)
	go burn(&wg)
	wg.Wait()
	return time.Since(start).Round(time.Millisecond)
}

func main() {
	fmt.Println("GOMAXPROCS=1 耗时:", run(1))
	fmt.Println("GOMAXPROCS=2 耗时:", run(2))
}
