package main

import (
	"sync"
	"testing"
)

type doc struct {
	id      string
	version int
	title   string
}

type searchStore struct {
	mu   sync.Mutex
	docs map[string]doc
}

func (s *searchStore) upsert(d doc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.docs[d.id]
	if ok && old.version >= d.version {
		return false // 乱序或重复事件，直接丢弃
	}
	s.docs[d.id] = d
	return true
}

func TestVersionGuardRejectsOutOfOrderEvents(t *testing.T) {
	s := &searchStore{docs: make(map[string]doc)}
	// 模拟重投与乱序：v3 先到，v2 与重复的 v3 后到
	if !s.upsert(doc{id: "1", version: 3, title: "v3"}) {
		t.Fatal("最新版本应被接受")
	}
	if s.upsert(doc{id: "1", version: 2, title: "v2"}) {
		t.Fatal("乱序旧版本不应覆盖新数据")
	}
	if s.upsert(doc{id: "1", version: 3, title: "v3-dup"}) {
		t.Fatal("重复版本不应被接受")
	}
	if s.docs["1"].title != "v3" {
		t.Fatalf("最终文档被污染: %+v", s.docs["1"])
	}
}
