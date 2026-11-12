package kv

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMemoryCRUD(t *testing.T) {
	cases := []struct {
		name    string
		cmd     Command
		want    string
		found   bool
		wantErr error
	}{
		{"set", Command{Op: OpSet, Key: "a", Value: []byte("1")}, "", true, nil},
		{"get", Command{Op: OpGet, Key: "a"}, "1", true, nil},
		{"del", Command{Op: OpDel, Key: "a"}, "", true, nil},
		{"miss", Command{Op: OpGet, Key: "a"}, "", false, ErrNotFound},
	}
	s := NewMemory()
	defer s.Close()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := s.Apply(c.cmd)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err=%v want=%v", err, c.wantErr)
			}
			if r.Found != c.found || string(r.Value) != c.want {
				t.Fatalf("got %+v", r)
			}
		})
	}
}

func TestConcurrentApply(t *testing.T) {
	s := NewShardedMemory(8)
	defer s.Close()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("k%d", i)
			if _, err := s.Apply(Command{Op: OpSet, Key: key, Value: []byte("v")}); err != nil {
				t.Errorf("set: %v", err)
			}
		}(i)
	}
	wg.Wait()
}

// tinyLegacy 复刻第 25 讲 MemStore 的方法集，并补一把锁：真实 MemStore
// 用 RWMutex 保护 map，测试替身也必须线程安全，因为 TTLMemory 的后台
// 回收会从另一个 goroutine 调用 inner.Apply。
type tinyLegacy struct {
	mu sync.Mutex
	m  map[string]string
}

func (l *tinyLegacy) Put(k, v string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.m[k] = v
	return nil
}
func (l *tinyLegacy) Get(k string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.m[k]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}
func (l *tinyLegacy) Delete(k string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, k)
	return nil
}
func (l *tinyLegacy) Keys() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.m))
	for k := range l.m {
		out = append(out, k)
	}
	return out
}

func TestWrapLegacyAndTTL(t *testing.T) {
	base := &tinyLegacy{m: make(map[string]string)}
	s := WrapLegacy(base, ErrNotFound)
	ttl := NewTTLMemory(s, 10*time.Millisecond)
	defer ttl.Close()
	if _, err := ttl.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
		t.Fatal(err)
	}
	ttl.SetTTL("k", 5*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	if _, err := ttl.Apply(Command{Op: OpGet, Key: "k"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
