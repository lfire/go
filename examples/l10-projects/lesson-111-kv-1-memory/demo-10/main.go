// 文件：cmd/lockprobe/main.go（独立程序，可直接 go run）
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// 只用于直观对比「一把全局锁」与「分片锁」在写密集下的差距。
func main() {
	const (
		goroutines = 8
		perG       = 200000
		shards     = 32
	)
	var global int64
	var globalMu sync.Mutex
	start := time.Now()
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				globalMu.Lock()
				global++
				globalMu.Unlock()
			}
		}()
	}
	wg.Wait()
	globalDur := time.Since(start)

	var shardLocks [shards]sync.Mutex
	var shardVals [shards]int64
	start = time.Now()
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				s := (g + i) % shards
				shardLocks[s].Lock()
				shardVals[s]++
				shardLocks[s].Unlock()
			}
		}(g)
	}
	wg.Wait()
	shardDur := time.Since(start)
	var total int64
	for i := 0; i < shards; i++ {
		total += atomic.LoadInt64(&shardVals[i])
	}
	fmt.Printf("global=%d %v  sharded=%d %v\n", global, globalDur, total, shardDur)
}
