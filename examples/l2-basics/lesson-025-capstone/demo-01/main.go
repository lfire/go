package main

import (
	"errors"
	"fmt"
	"sync"
)

// 三个包级哨兵错误：调用方用 errors.Is 判定，不比字符串。
var (
	ErrNotFound = errors.New("kv: key 不存在")
	ErrEmptyKey = errors.New("kv: key 不能为空")
)

// Storage 是 KV 对外的最小契约：上层只依赖它，不依赖 map。
type Storage interface {
	Put(key, value string) error
	Get(key string) (string, error)
	Delete(key string) error
	Keys() []string
}

// MemStore 是内存实现：一张 map 加一把读写锁。
type MemStore struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewMemStore 返回具体类型，调用方按需赋给接口变量。
func NewMemStore() *MemStore {
	return &MemStore{data: make(map[string]string)}
}

func (s *MemStore) Put(key, value string) error {
	if key == "" {
		return ErrEmptyKey
	}
	s.mu.Lock()
	defer s.mu.Unlock() // 第 021 讲：解锁一定执行
	s.data[key] = value
	return nil
}

func (s *MemStore) Get(key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key] // 第 016 讲：comma-ok
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (s *MemStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; !ok {
		return ErrNotFound
	}
	delete(s.data, key)
	return nil
}

func (s *MemStore) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.data))
	for k := range s.data {
		out = append(out, k)
	}
	return out
}

func main() {
	var store Storage = NewMemStore() // 声明为接口，换实现不动这里
	_ = store.Put("lang", "go")
	v, err := store.Get("lang")
	fmt.Println(v, err == nil) // go true

	_, err = store.Get("missing")
	fmt.Println(errors.Is(err, ErrNotFound)) // true

	_ = store.Delete("lang")
	fmt.Println(len(store.Keys())) // 0
}
