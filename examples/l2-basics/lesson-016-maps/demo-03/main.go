package main

import (
	"fmt"
	"sync"
)

func main() {
	var mu sync.Mutex
	m := make(map[string]int)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			m["hits"]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	mu.Lock()
	fmt.Println("hits =", m["hits"])
	mu.Unlock()
}
