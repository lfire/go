package main

import (
	"sync"
	"sync/atomic"
	"testing"
)

type consumer struct {
	mu      sync.Mutex
	seen    map[string]bool
	applied int32
}

func (c *consumer) handle(eventID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen[eventID] {
		return // 命中去重，仍应向上层确认
	}
	c.seen[eventID] = true
	atomic.AddInt32(&c.applied, 1)
}

func TestIdempotentConsumerCollapsesDuplicates(t *testing.T) {
	c := &consumer{seen: make(map[string]bool)}
	const duplicates = 20
	var wg sync.WaitGroup
	for i := 0; i < duplicates; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.handle("evt-1001")
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&c.applied); got != 1 {
		t.Fatalf("业务被应用 %d 次，应为 1", got)
	}
}
