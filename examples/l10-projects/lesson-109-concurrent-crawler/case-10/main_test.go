package main

import (
	"fmt"
	"sync"
	"testing"
)

// visitSet 是并发安全的去重替身，替代生产里的 Redis SETNX。
type visitSet struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func newVisitSet() *visitSet { return &visitSet{seen: map[string]struct{}{}} }

func (s *visitSet) Visit(u string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[u]; ok {
		return false
	}
	s.seen[u] = struct{}{}
	return true
}

// normalize 去掉 URL 片段，避免同一页带不同锚点被当成多页。
func normalize(raw string) string {
	for i := 0; i < len(raw); i++ {
		if raw[i] == '#' {
			return raw[:i]
		}
	}
	return raw
}

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/a#top", "/a"},
		{"/b", "/b"},
		{"/c?sid=1#x", "/c?sid=1"},
	}
	for _, tc := range cases {
		if got := normalize(tc.in); got != tc.want {
			t.Errorf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestVisitConcurrent(t *testing.T) {
	s := newVisitSet()
	first := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.Visit("/hot") {
				mu.Lock()
				first++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if first != 1 {
		t.Fatalf("首次调度者 = %d, 期望 1（go test -race 应无报告）", first)
	}
}

func BenchmarkVisit(b *testing.B) {
	s := newVisitSet()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			s.Visit(fmt.Sprintf("/u/%d", i))
			i++
		}
	})
}
