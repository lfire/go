package main

import (
	"fmt"
	"sync"
	"time"
)

// Coordinator 模拟 etcd 选主核心语义：租约、续租、epoch、优雅退位。
type Coordinator struct {
	mu      sync.Mutex
	holder  string
	leaseID int64
	expiry  time.Time
	epoch   int64
	nextID  int64
}

func NewCoordinator() *Coordinator { return &Coordinator{} }

// Campaign 抢主：已有存活 leader 则失败，否则授予租约并递增 epoch。
func (c *Coordinator) Campaign(node string, ttl time.Duration) (int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.holder != "" && now.Before(c.expiry) {
		return 0, false
	}
	c.nextID++
	c.leaseID = c.nextID
	c.holder = node
	c.expiry = now.Add(ttl)
	c.epoch++
	fmt.Printf("  %s 当选 租约=%d epoch=%d\n", node, c.leaseID, c.epoch)
	return c.leaseID, true
}

// KeepAlive 只有当前 holder 能续自己的租约。
func (c *Coordinator) KeepAlive(node string, id int64, ttl time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.holder != node || c.leaseID != id || time.Now().After(c.expiry) {
		return false
	}
	c.expiry = time.Now().Add(ttl)
	return true
}

// IsLeader 用 fencing epoch 判定派发资格，挡旧 leader 复活。
func (c *Coordinator) IsLeader(node string, epoch int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.holder == node && c.epoch == epoch && time.Now().Before(c.expiry)
}

// Resign 主动释放租约，让跟随者不必等 TTL（优雅退位）。
func (c *Coordinator) Resign(node string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.holder == node {
		c.holder = ""
		fmt.Println("  ", node, "主动退位，跟随者立即接管")
	}
}

func main() {
	c := NewCoordinator()
	ttl := 30 * time.Millisecond
	id, _ := c.Campaign("master-1", ttl)
	_, won := c.Campaign("master-2", ttl)
	fmt.Println("master-2 抢主成功:", won)
	fmt.Println("master-1 续租:", c.KeepAlive("master-1", id, ttl))
	fmt.Println("master-1 有派发权:", c.IsLeader("master-1", 1))

	time.Sleep(ttl + 5*time.Millisecond) // 模拟进程卡死，停止续租
	fmt.Println("过期后 master-1 仍有派发权:", c.IsLeader("master-1", 1))
	_, ok := c.Campaign("master-2", ttl)
	fmt.Println("master-2 重新竞选:", ok)
	fmt.Println("旧 leader 用旧 epoch 派发被拒:", !c.IsLeader("master-1", 1))
	fmt.Println("master-2 用新 epoch 派发:", c.IsLeader("master-2", 2))
	c.Resign("master-2")
}
