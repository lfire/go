package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

type idempotentStore struct {
	mu      sync.Mutex
	seen    map[string]int
	creates int32
}

func (s *idempotentStore) handle(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	s.mu.Lock()
	if code, ok := s.seen[key]; ok {
		s.mu.Unlock()
		w.WriteHeader(code)
		return
	}
	s.seen[key] = http.StatusCreated
	s.mu.Unlock()
	atomic.AddInt32(&s.creates, 1)
	w.WriteHeader(http.StatusCreated)
}

func TestIdempotencyKeyUnderConcurrency(t *testing.T) {
	s := &idempotentStore{seen: make(map[string]int)}
	srv := httptest.NewServer(http.HandlerFunc(s.handle))
	defer srv.Close()
	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
			req.Header.Set("Idempotency-Key", "order-1001")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			_ = resp.Body.Close()
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&s.creates); got != 1 {
		t.Fatalf("相同幂等键创建了 %d 次，应为 1", got)
	}
}
