package main

import (
	"errors"
	"sync"
	"testing"
)

var errUsage = errors.New("usage")

func code(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, errUsage) {
		return 2
	}
	return 1
}

type memStore struct {
	mu    sync.Mutex
	items []int
	next  int
}

func (m *memStore) Add() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	m.items = append(m.items, m.next)
	return m.next
}

func TestExitCode(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{errUsage, 2},
		{errors.New("io"), 1},
	}
	for _, c := range cases {
		if got := code(c.err); got != c.want {
			t.Errorf("code(%v)=%d, want %d", c.err, got, c.want)
		}
	}
}

func TestConcurrentAddUniqueID(t *testing.T) {
	m := &memStore{}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Add()
		}()
	}
	wg.Wait()
	seen := map[int]bool{}
	for _, id := range m.items {
		if seen[id] {
			t.Fatalf("重复 ID %d, 说明 maxID 加一未加锁", id)
		}
		seen[id] = true
	}
	if len(seen) != 100 {
		t.Fatalf("期望 100 个唯一 ID, 实际 %d", len(seen))
	}
}
