package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrNotFound = errors.New("短链不存在")
	ErrExpired  = errors.New("短链已过期")
)

type Entry struct {
	URL     string
	Expires time.Time
}

type Store struct {
	mu   sync.RWMutex
	data map[string]Entry
}

func NewStore() *Store {
	return &Store{data: make(map[string]Entry)}
}

func (s *Store) Put(code, url string, ttl time.Duration) {
	e := Entry{URL: url}
	if ttl > 0 {
		e.Expires = time.Now().Add(ttl)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[code] = e
}

// Get 过期判断放在锁内，避免返回一条本该失效的脏数据。
func (s *Store) Get(code string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.data[code]
	if !ok {
		return "", ErrNotFound
	}
	if !e.Expires.IsZero() && time.Now().After(e.Expires) {
		return "", ErrExpired
	}
	return e.URL, nil
}

// Janitor 后台清理；ctx 取消时退出，wg.Done 保证 goroutine 被回收。
func (s *Store) Janitor(ctx context.Context, interval time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.mu.Lock()
			for k, e := range s.data {
				if !e.Expires.IsZero() && now.After(e.Expires) {
					delete(s.data, k)
				}
			}
			s.mu.Unlock()
		}
	}
}

func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data)
}

func main() {
	s := NewStore()
	s.Put("aB3", "https://example.com/a", 50*time.Millisecond)
	s.Put("keep", "https://go.dev", 0)
	u, err := s.Get("aB3")
	fmt.Println("立即读:", u, err)
	time.Sleep(60 * time.Millisecond)
	_, err = s.Get("aB3")
	fmt.Println("过期后:", errors.Is(err, ErrExpired))

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go s.Janitor(ctx, 20*time.Millisecond, &wg)
	time.Sleep(50 * time.Millisecond)
	cancel()
	wg.Wait() // 确认后台 goroutine 已退出，避免资源泄漏
	fmt.Println("清理后剩余条目:", s.Len())
}
