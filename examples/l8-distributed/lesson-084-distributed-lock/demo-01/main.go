package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// leaseLock 用内存 map 模拟 Redis 的 SET key value NX EX 语义
type leaseLock struct {
	mu     sync.Mutex
	holder map[string]holder
}

// holder 是锁记录：令牌证明归属，过期时间实现租约
type holder struct {
	token  string
	expire time.Time
}

func newToken() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Acquire 仅在键不存在或已过期时写入，返回本次持有的令牌
func (l *leaseLock) Acquire(key string, ttl time.Duration) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if h, ok := l.holder[key]; ok && time.Now().Before(h.expire) {
		return "", false // 锁仍被别人持有
	}
	t := newToken()
	l.holder[key] = holder{token: t, expire: time.Now().Add(ttl)}
	return t, true
}

// Release 先比对令牌再删除，模拟上面那段 Lua 的原子语义
func (l *leaseLock) Release(key, token string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	h, ok := l.holder[key]
	if !ok || h.token != token {
		return false // 令牌不匹配，说明锁已易主，绝不能删
	}
	delete(l.holder, key)
	return true
}

func main() {
	l := &leaseLock{holder: make(map[string]holder)}
	key := "order:1001"

	tokA, ok := l.Acquire(key, 200*time.Millisecond)
	fmt.Println("A 加锁:", ok)

	_, ok = l.Acquire(key, 200*time.Millisecond)
	fmt.Println("B 并发加锁:", ok, "(被拒绝)")

	// 模拟 A 业务卡顿超过租约：锁到期后 B 能拿到锁
	time.Sleep(250 * time.Millisecond)
	tokB, ok := l.Acquire(key, 200*time.Millisecond)
	fmt.Println("租约过期后 B 加锁:", ok)

	// A 恢复后试图释放，令牌不匹配，不会误删 B 的锁
	fmt.Println("A 用旧令牌释放:", l.Release(key, tokA), "(被拒绝)")
	fmt.Println("B 用自己的令牌释放:", l.Release(key, tokB))
}
