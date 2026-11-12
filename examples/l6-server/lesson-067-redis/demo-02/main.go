package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type entry struct {
	value   string
	expires time.Time
}

type cache struct {
	mu    sync.Mutex
	data  map[string]entry
	loads sync.Mutex // 简化版单飞：同一时刻只允许一个协程回源
}

func (c *cache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.data[key]
	if !ok || time.Now().After(e.expires) {
		return "", false
	}
	return e.value, true
}

func (c *cache) set(key, value string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = entry{value: value, expires: time.Now().Add(ttl)}
}

func ttlWithJitter(base time.Duration) time.Duration {
	return base + time.Duration(rand.Int63n(int64(base/5)))
}

// loadWithSingleFlight 让并发的未命中请求只回源一次。
func (c *cache) loadWithSingleFlight(key string, load func() string) string {
	if v, ok := c.get(key); ok {
		return v
	}
	c.loads.Lock()
	defer c.loads.Unlock()
	// 双检：等在锁上的请求进来时，可能已被前一个协程重建好了。
	if v, ok := c.get(key); ok {
		return v
	}
	v := load()
	c.set(key, v, ttlWithJitter(3*time.Second))
	return v
}

func main() {
	c := &cache{data: make(map[string]entry)}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.loadWithSingleFlight("hot", func() string {
				fmt.Println("回源一次（模拟慢查询）")
				time.Sleep(20 * time.Millisecond)
				return "value"
			})
		}()
	}
	wg.Wait()
	fmt.Println("20 个并发请求，回源只发生 1 次")
}
