package main

import (
	"fmt"
	"sync"
	"time"
)

// Election 模拟 etcd 选主：一个带租约的键，谁先创建谁当主
type Election struct {
	mu       sync.Mutex
	leader   string
	deadline time.Time
	watches  []chan string
}

// Campaign 尝试抢主：键不存在或租约过期时用 CAS 写入自己
func (e *Election) Campaign(node string, ttl time.Duration) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	if e.leader != "" && now.Before(e.deadline) {
		return false // 主还活着
	}
	prev := e.leader
	e.leader = node
	e.deadline = now.Add(ttl)
	e.notify("leader=" + node)
	fmt.Println(node, "抢主成功，前任:", prev)
	return true
}

// KeepAlive 主节点续租，续不上就说明租约已丢
func (e *Election) KeepAlive(node string, ttl time.Duration) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.leader != node || time.Now().After(e.deadline) {
		return false
	}
	e.deadline = time.Now().Add(ttl)
	return true
}

// Watch 返回事件通道，模拟 etcd 的 watch
func (e *Election) Watch() <-chan string {
	ch := make(chan string, 8)
	e.mu.Lock()
	e.watches = append(e.watches, ch)
	e.mu.Unlock()
	return ch
}

func (e *Election) notify(ev string) {
	for _, ch := range e.watches {
		select {
		case ch <- ev:
		default:
		}
	}
}

func main() {
	e := &Election{}
	w := e.Watch()

	e.Campaign("n1", 300*time.Millisecond)
	fmt.Println("n1 正常续租:", e.KeepAlive("n1", 300*time.Millisecond))
	fmt.Println("n2 并发抢主:", e.Campaign("n2", 300*time.Millisecond))

	// 模拟 n1 宕机：停止续租，等租约自然过期
	time.Sleep(350 * time.Millisecond)
	fmt.Println("n2 在租约过期后抢主:", e.Campaign("n2", 300*time.Millisecond))

	// watch 收到两次主变更
	fmt.Println("watch 事件:", <-w, <-w)
}
