package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// keeper 模拟看门狗：业务未结束就持续把租约续长
type keeper struct {
	mu      sync.Mutex
	expires time.Time
	ttl     time.Duration
}

// renew 续期；若锁已经丢失，续期无意义，必须让业务中止
func (k *keeper) renew() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if time.Now().After(k.expires) {
		return false
	}
	k.expires = time.Now().Add(k.ttl)
	return true
}

func (k *keeper) remaining() time.Duration {
	k.mu.Lock()
	defer k.mu.Unlock()
	return time.Until(k.expires)
}

// watchdog 每 ttl/3 续一次，业务结束或续期失败即退出
func (k *keeper) watchdog(ctx context.Context, done chan<- string) {
	ticker := time.NewTicker(k.ttl / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- "业务结束，停止续期"
			return
		case <-ticker.C:
			if !k.renew() {
				done <- "续期失败：锁已丢失，立即中止业务"
				return
			}
		}
	}
}

func main() {
	k := &keeper{ttl: 300 * time.Millisecond, expires: time.Now().Add(300 * time.Millisecond)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan string, 1)
	go k.watchdog(ctx, done)

	// 模拟一个耗时远超初始租约的业务
	time.Sleep(700 * time.Millisecond)
	fmt.Println("业务执行 700ms 后锁仍有效:", k.remaining() > 0)
	cancel()
	fmt.Println(<-done)
}
