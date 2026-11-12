package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// Config 是一份不可变快照；Namespace 用环境与服务两级隔离。
type Config struct {
	Namespace string
	Version   int
	Data      map[string]string
}

// Store 保存当前值与历史版本，支持热更新与按版本回滚。
type Store struct {
	mu      sync.RWMutex
	current atomic.Value // 存 *Config，读路径完全无锁
	history []*Config
}

func NewStore(init *Config) *Store {
	s := &Store{}
	s.current.Store(init)
	s.history = append(s.history, init)
	return s
}

func (s *Store) Get() *Config { return s.current.Load().(*Config) }

func (s *Store) Publish(data map[string]string) *Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.Get()
	next := &Config{Namespace: old.Namespace, Version: old.Version + 1, Data: data}
	s.current.Store(next) // 原子替换整份快照，业务无需停服
	s.history = append(s.history, next)
	return next
}

func (s *Store) Rollback(version int) (*Config, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.history {
		if c.Version == version {
			s.current.Store(c)
			return c, true
		}
	}
	return nil, false
}

func main() {
	s := NewStore(&Config{Namespace: "prod/order", Version: 1,
		Data: map[string]string{"timeout_ms": "500"}})
	fmt.Println("初始:", s.Get().Data["timeout_ms"], "版本", s.Get().Version)
	s.Publish(map[string]string{"timeout_ms": "800"})
	fmt.Println("热更后:", s.Get().Data["timeout_ms"], "版本", s.Get().Version)
	c, _ := s.Rollback(1)
	fmt.Println("回滚后:", c.Data["timeout_ms"], "版本", c.Version)
}
