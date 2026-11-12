package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

type Config struct{ Version, RateLimit int }

var cur atomic.Pointer[Config]

func tryUpdate(next *Config) (retries int, ok bool) {
	for {
		old := cur.Load()
		runtime.Gosched() // 仅实验：放大 Load 与 CAS 之间的窗口
		if old != nil && next.Version <= old.Version {
			return retries, false // 版本不够新，直接放弃
		}
		if cur.CompareAndSwap(old, next) {
			return retries, true
		}
		retries++ // CAS 失败：别人抢先改了，重读再试
	}
}

func main() {
	cur.Store(&Config{Version: 0, RateLimit: 100})
	const N = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for v := 1; v <= N; v++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			<-start
			r, _ := tryUpdate(&Config{Version: v, RateLimit: v * 10})
			mu.Lock()
			total += r
			mu.Unlock()
		}(v)
	}
	close(start)
	wg.Wait()
	fmt.Printf("并发 %d 个 CAS 更新，累计重试次数 = %d\n", N, total)
	fmt.Printf("最终快照 = %+v\n", *cur.Load())
	fmt.Println("最终 Version 是否达到最大值:", cur.Load().Version == N)
}
