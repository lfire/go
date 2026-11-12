package main

import (
	"os"
	"runtime"
	"runtime/trace"
	"sync"
)

func main() {
	f, err := os.Create("trace.out")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := trace.Start(f); err != nil {
		panic(err)
	}
	defer trace.Stop()

	var wg sync.WaitGroup
	for i := 0; i < runtime.GOMAXPROCS(0)*4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sum := 0
			for j := 0; j < 1_000_000; j++ {
				sum += j % 7
			}
			runtime.KeepAlive(sum)
		}()
	}
	wg.Wait()
}
