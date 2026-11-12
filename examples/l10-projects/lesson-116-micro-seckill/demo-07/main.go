// 文件：reconcile.go —— 超时释放与对账：条件更新防已支付后释放
package main

import (
	"fmt"
	"sync"
	"time"
)

type resState uint8

const (
	reserved resState = iota + 1
	paid
	released
)

type record struct {
	userID    string
	state     resState
	createdAt time.Time
}

// Ledger 同时保存预扣记录与库存水位；所有读状态加改状态都在同一把锁内。
type Ledger struct {
	mu      sync.Mutex
	left    int
	records map[string]record
}

func NewLedger(total int) *Ledger {
	return &Ledger{left: total, records: make(map[string]record)}
}

func (l *Ledger) Reserve(userID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.left <= 0 {
		return false
	}
	l.left--
	l.records[userID] = record{userID: userID, state: reserved, createdAt: time.Now()}
	return true
}

// MarkPaid 与 Release 都是条件更新：仅当状态仍为 reserved 才生效。
// 支付先到则 Release 不回补，释放先到则支付被拒，杜绝竞态。
func (l *Ledger) MarkPaid(userID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.records[userID]
	if !ok || r.state != reserved {
		return false
	}
	r.state = paid
	l.records[userID] = r
	return true
}

func (l *Ledger) Release(userID string, timeout time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.records[userID]
	if !ok || r.state != reserved || time.Since(r.createdAt) < timeout {
		return false
	}
	r.state = released
	l.records[userID] = r
	l.left++
	return true
}

// Reconcile 扫描所有预留中记录，释放超时的；返回释放条数供对账断言。
func (l *Ledger) Reconcile(timeout time.Duration) (freed int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, r := range l.records {
		if r.state == reserved && time.Since(r.createdAt) >= timeout {
			r.state = released
			l.records[id] = r
			l.left++
			freed++
		}
	}
	return freed
}

func (l *Ledger) Left() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.left
}

func main() {
	led := NewLedger(100)
	led.Reserve("u-1")
	led.Reserve("u-2")
	led.MarkPaid("u-1") // 已支付
	n := led.Reconcile(time.Nanosecond)
	_ = led.Release("u-1", time.Nanosecond) // 已支付记录不应被回补
	fmt.Printf("freed=%d left=%d want=1/99\n", n, led.Left())
}
