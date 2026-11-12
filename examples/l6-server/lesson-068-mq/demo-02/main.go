package main

import (
	"fmt"
	"sync"
)

type Event struct {
	ID     string // 幂等键：业务唯一 ID，不是中间件给的 offset
	Order  string
	Amount int
}

type Consumer struct {
	mu     sync.Mutex
	seen   map[string]bool // 模拟去重表；生产用数据库唯一索引
	orders map[string]int  // 模拟业务状态
}

func NewConsumer() *Consumer {
	return &Consumer{
		seen:   map[string]bool{},
		orders: map[string]int{},
	}
}

// Handle 保证同一 Event.ID 无论投递多少次，业务状态只变更一次。
func (c *Consumer) Handle(e Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen[e.ID] {
		fmt.Println("重复投递，直接确认:", e.ID)
		return
	}
	// 生产里「判重 + 写业务 + 记去重」必须放在同一个数据库事务中。
	c.orders[e.Order] += e.Amount
	c.seen[e.ID] = true
	fmt.Println("已处理:", e.ID, "累计金额:", c.orders[e.Order])
}

func main() {
	c := NewConsumer()
	e := Event{ID: "evt-1001", Order: "order-1", Amount: 30}
	c.Handle(e) // 模拟 at-least-once 下的三次重复投递
	c.Handle(e)
	c.Handle(e)
	fmt.Println("最终累计金额:", c.orders["order-1"], "（应为 30 而非 90）")
}
