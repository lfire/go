package main

import (
	"errors"
	"fmt"
	"sync"
)

var ErrNotFound = errors.New("kv: key 不存在")

type Storage interface {
	Put(key, value string) error
	Get(key string) (string, error)
}

type MemStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func (s *MemStore) Put(k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[k] = v
	return nil
}

func (s *MemStore) Get(k string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[k]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

// fakeStore 是单测替身：接口让 Apply 不必起真实存储。
type fakeStore struct{}

func (fakeStore) Put(k, v string) error { return nil }
func (fakeStore) Get(k string) (string, error) {
	return "fake", nil
}

func Apply(s Storage, put bool, k, v string) (string, error) {
	if put {
		return "OK", s.Put(k, v)
	}
	return s.Get(k)
}

func main() {
	s := &MemStore{data: make(map[string]string)}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = s.Put(fmt.Sprintf("k%d", n), "v")
		}(i)
	}
	wg.Wait()
	v, err := s.Get("k7")
	fmt.Println("并发写后取 k7 =", v, "出错 =", err != nil)
	out, _ := Apply(fakeStore{}, false, "x", "")
	fmt.Println("替身测试 =", out)
}
