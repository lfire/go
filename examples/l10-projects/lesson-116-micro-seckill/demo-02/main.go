// 文件：stock.go —— 原子预扣：检查、判重、扣减同临界区
package main

import (
	"errors"
	"fmt"
	"sync"
)

var (
	ErrSoldOut       = errors.New("seckill: sold out")
	ErrNoReservation = errors.New("seckill: no reservation")
	ErrAlreadyPaid   = errors.New("seckill: already paid")
)

type resState uint8

const (
	resReserved resState = iota + 1
	resPaid
	resReleased
)

type reservation struct {
	userID string
	state  resState
}

// Stock 用一把锁把检查、判重、扣减合成一个原子操作，
// 对应 Redis 里一条 Lua 脚本，杜绝先查后扣造成的超卖。
type Stock struct {
	mu      sync.Mutex
	left    int
	records map[string]reservation
}

func NewStock(total int) *Stock {
	return &Stock{left: total, records: make(map[string]reservation)}
}

// Deduct 返回是否本次新抢到。四步处在同一临界区，形成严格串行序列。
func (s *Stock) Deduct(userID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[userID]; ok {
		return false, nil // 幂等：重复请求返回首次结果，不再扣减
	}
	if s.left <= 0 {
		return false, ErrSoldOut
	}
	s.left--
	s.records[userID] = reservation{userID: userID, state: resReserved}
	return true, nil
}

// MarkPaid 用条件更新把 RESERVED 置为 PAID；只有当前仍是预留中才成功，
// 这样超时释放任务与支付不会竞态把库存凭空加回。
func (s *Stock) MarkPaid(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[userID]
	if !ok || r.state != resReserved {
		return ErrAlreadyPaid
	}
	r.state = resPaid
	s.records[userID] = r
	return nil
}

// Refund 条件回补：仅当状态仍为 RESERVED 时释放，拒绝空回补与已支付回补。
func (s *Stock) Refund(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[userID]
	if !ok || r.state != resReserved {
		return ErrNoReservation
	}
	s.left++
	r.state = resReleased
	s.records[userID] = r
	return nil
}

func (s *Stock) Left() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.left
}

func main() {
	s := NewStock(100)
	granted := 0
	for i := 0; i < 1000; i++ {
		ok, _ := s.Deduct(fmt.Sprintf("u-%d", i))
		if ok {
			granted++
		}
	}
	fmt.Printf("granted=%d left=%d\n", granted, s.Left())
	_ = s.Refund("u-0")
	fmt.Printf("after refund left=%d\n", s.Left())
	_ = s.Refund("u-0") // 重复回补被挡
	fmt.Printf("after duplicate refund left=%d\n", s.Left())
}
