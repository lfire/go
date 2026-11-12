package main

import (
	"fmt"
	"sync"
	"time"
)

type Config struct {
	Version int
	Data    map[string]string
}

// Watcher 用「携带版本号的等待」模拟长轮询：没变更就挂住，变了立刻返回新快照。
type Watcher struct {
	mu      sync.Mutex
	version int
	data    map[string]string
	waiters []chan Config
}

func (w *Watcher) LongPoll(knownVersion int) <-chan Config {
	ch := make(chan Config, 1)
	w.mu.Lock()
	if w.version > knownVersion { // 已有更新，立即返回
		ch <- Config{Version: w.version, Data: w.data}
	} else {
		w.waiters = append(w.waiters, ch) // 挂住，等变更唤醒
	}
	w.mu.Unlock()
	return ch
}

func (w *Watcher) Update(data map[string]string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.version++
	w.data = data
	for _, ch := range w.waiters {
		ch <- Config{Version: w.version, Data: w.data}
	}
	w.waiters = nil
}

func main() {
	w := &Watcher{version: 1, data: map[string]string{"rate": "10"}}
	// 客户端带上版本 1 来等，此时无更新，请求被挂住
	pending := w.LongPoll(1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		w.Update(map[string]string{"rate": "20"})
	}()
	cfg := <-pending // 变更发生时才返回
	fmt.Printf("长轮询收到 版本=%d rate=%s\n", cfg.Version, cfg.Data["rate"])
}
