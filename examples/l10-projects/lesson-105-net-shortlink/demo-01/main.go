package main

import (
	"fmt"
	"time"
)

// Link 是短链服务对外的数据契约，短码为主键。
type Link struct {
	Code    string
	Target  string
	Expires time.Time
}

// Store 定义唯一可信数据源，读多写少，接口刻意做小。
type Store interface {
	Put(l Link) error
	Get(code string) (Link, error)
	Delete(code string) error
}

// Allocator 只负责发号，不负责编码，便于独立替换成号段或雪花实现。
type Allocator interface {
	Next() (uint64, error)
}

// Cache 抽象缓存旁路，Get 的布尔返回值区分未命中与空值。
type Cache interface {
	Get(key string) (string, bool)
	Set(key, val string, ttl time.Duration)
	Del(key string)
}

// memStore 是接口的最小实现，演示依赖倒置：上层只依赖接口。
type memStore struct {
	data map[string]Link
}

func (m *memStore) Put(l Link) error {
	if l.Code == "" {
		return fmt.Errorf("空短码拒绝落库")
	}
	m.data[l.Code] = l
	return nil
}

func (m *memStore) Get(code string) (Link, error) {
	l, ok := m.data[code]
	if !ok {
		return Link{}, fmt.Errorf("短链不存在")
	}
	return l, nil
}

func (m *memStore) Delete(code string) error {
	delete(m.data, code)
	return nil
}

func main() {
	var s Store = &memStore{data: make(map[string]Link)}
	_ = s.Put(Link{Code: "1", Target: "https://go.dev"})
	l, err := s.Get("1")
	fmt.Println("命中:", l.Target, err)
}
