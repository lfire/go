package kv

import (
	"sync"
	"time"
)

var _ Storage = (*TTLMemory)(nil)

// TTLMemory 是叠加在任意 Storage 之外的过期装饰器。
// 过期表与命令解耦，SetTTL 单独登记，不污染四讲锁定的 Command 字段集合。
type TTLMemory struct {
	inner    Storage
	mu       sync.Mutex
	expireAt map[string]time.Time
	quit     chan struct{}
	wg       sync.WaitGroup
	closed   bool
	interval time.Duration
}

func NewTTLMemory(inner Storage, interval time.Duration) *TTLMemory {
	if interval <= 0 {
		interval = time.Second
	}
	t := &TTLMemory{
		inner:    inner,
		expireAt: make(map[string]time.Time),
		quit:     make(chan struct{}),
		interval: interval,
	}
	t.wg.Add(1)
	go t.reapLoop()
	return t
}

func (t *TTLMemory) Apply(cmd Command) (Result, error) {
	if cmd.Op == OpGet && t.isExpired(cmd.Key) {
		// DEL 幂等：惰性删除与后台回收撞车也不会出错。
		_, _ = t.inner.Apply(Command{Op: OpDel, Key: cmd.Key})
		return Result{}, ErrNotFound
	}
	return t.inner.Apply(cmd)
}

func (t *TTLMemory) isExpired(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	deadline, ok := t.expireAt[key]
	if !ok {
		return false
	}
	// time.Now 含单调时钟分量，墙钟回拨不会让未过期的键被误删。
	if time.Now().Before(deadline) {
		return false
	}
	delete(t.expireAt, key)
	return true
}

func (t *TTLMemory) SetTTL(key string, d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.expireAt[key] = time.Now().Add(d)
}

func (t *TTLMemory) reapLoop() {
	defer t.wg.Done()
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.mu.Lock()
			keys := make([]string, 0, len(t.expireAt))
			now := time.Now()
			for k, deadline := range t.expireAt {
				if !now.Before(deadline) {
					keys = append(keys, k)
				}
			}
			t.mu.Unlock()
			for _, k := range keys {
				_, _ = t.inner.Apply(Command{Op: OpDel, Key: k})
			}
		case <-t.quit:
			return
		}
	}
}

func (t *TTLMemory) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	close(t.quit)
	t.mu.Unlock()
	t.wg.Wait()
	return t.inner.Close()
}
