// 文件：idempotency.go —— 幂等键三态存储，防重复、防并发、带 TTL 清理
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrInProgress = errors.New("idempotency: request in progress")

type idemState uint8

const (
	stateInProgress idemState = iota + 1
	stateDone
	stateFailed
)

type record struct {
	state   idemState
	payload string
	expire  time.Time
}

// IdemStore 用一把锁保护去重表。生产里换成 MySQL 唯一索引
// uk_idem(idem_key) 加 INSERT ... ON DUPLICATE KEY，语义完全一致。
type IdemStore struct {
	mu      sync.Mutex
	records map[string]record
	ttl     time.Duration
	now     func() time.Time
}

func NewIdemStore(ttl time.Duration) *IdemStore {
	return &IdemStore{records: make(map[string]record), ttl: ttl, now: time.Now}
}

// Begin 抢占幂等键：已 DONE 直接返回首次结果（幂等成功）；
// 仍 IN_PROGRESS 说明前一个请求未结束，拒绝并发重复；
// FAILED 或已过期则允许重新进入。
func (s *IdemStore) Begin(key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if r, ok := s.records[key]; ok && now.Before(r.expire) {
		switch r.state {
		case stateDone:
			return r.payload, true, nil
		case stateInProgress:
			return "", false, ErrInProgress
		}
	}
	s.records[key] = record{state: stateInProgress, expire: now.Add(s.ttl)}
	return "", false, nil
}

func (s *IdemStore) Commit(key, payload string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[key] = record{state: stateDone, payload: payload, expire: s.now().Add(s.ttl)}
}

func (s *IdemStore) Abort(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[key] = record{state: stateFailed, expire: s.now().Add(s.ttl)}
}

func (s *IdemStore) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, r := range s.records {
		if !now.Before(r.expire) {
			delete(s.records, k)
		}
	}
}

func main() {
	s := NewIdemStore(24 * time.Hour)
	_, done, err := s.Begin("idem-abc")
	fmt.Printf("first: done=%v err=%v\n", done, err)
	s.Commit("idem-abc", "O-1001")
	payload, done, err := s.Begin("idem-abc")
	fmt.Printf("retry: payload=%s done=%v err=%v\n", payload, done, err)

	// 并发重复：同一键只应有一个请求进入临界区。
	var wg sync.WaitGroup
	var mu sync.Mutex
	entered := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, d, e := s.Begin("idem-concurrent")
			if e == nil && !d {
				mu.Lock()
				entered++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	fmt.Printf("concurrent entered=%d want=1\n", entered)
	s.Sweep()
}
