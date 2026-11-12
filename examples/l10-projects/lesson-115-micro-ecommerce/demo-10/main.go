// 文件：refund_idempotent.go —— 用唯一键把退款补偿变成幂等操作
package main

import (
	"fmt"
	"sync"
)

// refundStore 用唯一键模拟退款流水表；Once 对应
// INSERT ... ON DUPLICATE KEY，影响行数为 0 说明已退过。
type refundStore struct {
	mu   sync.Mutex
	seen map[string]int
}

func newRefundStore() *refundStore {
	return &refundStore{seen: make(map[string]int)}
}

// Once 返回本次是否真的执行退款；重复键返回 false 且不扣第二笔。
func (s *refundStore) Once(idemKey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[idemKey] > 0 {
		return false
	}
	s.seen[idemKey] = 1
	return true
}

// RefundRight 以幂等键去重，重复补偿直接返回「已处理」。
func RefundRight(s *refundStore, idemKey, orderID string) bool {
	return s.Once(idemKey)
}

func main() {
	s := newRefundStore()
	first := RefundRight(s, "idem-abc", "O-1001")
	retry := RefundRight(s, "idem-abc", "O-1001") // 模拟补偿重试
	fmt.Printf("first=%v retry=%v total=%d want=1\n", first, retry, s.seen["idem-abc"])
}
