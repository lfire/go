// 文件：stock_test.go —— 表驱动覆盖不超卖、回补幂等与超时释放竞态
package seckill

import (
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var errSoldOut = errors.New("sold out")

type resState uint8

const (
	reserved resState = iota + 1
	paid
	released
)

type record struct {
	state resState
	at    time.Time
}

type stock struct {
	mu      sync.Mutex
	remain  int
	records map[string]record
}

func newStock(n int) *stock {
	return &stock{remain: n, records: make(map[string]record)}
}

func (s *stock) deduct(u string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[u]; ok {
		return false, nil
	}
	if s.remain <= 0 {
		return false, errSoldOut
	}
	s.remain--
	s.records[u] = record{state: reserved, at: time.Now()}
	return true, nil
}

func (s *stock) markPaid(u string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[u]
	if !ok || r.state != reserved {
		return false
	}
	r.state = paid
	s.records[u] = r
	return true
}

func (s *stock) release(u string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[u]
	if !ok || r.state != reserved {
		return false
	}
	r.state = released
	s.records[u] = r
	s.remain++
	return true
}

func (s *stock) left() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remain
}

func TestNoOversell(t *testing.T) {
	cases := []struct{ total, users int }{{100, 1000}, {1, 50}, {0, 10}}
	for _, c := range cases {
		s := newStock(c.total)
		var success int64
		var wg sync.WaitGroup
		for i := 0; i < c.users; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				if ok, _ := s.deduct("u" + strconv.Itoa(id)); ok {
					atomic.AddInt64(&success, 1)
				}
			}(i)
		}
		wg.Wait()
		want := int64(c.total)
		if c.total > c.users {
			want = int64(c.users)
		}
		if success != want {
			t.Fatalf("total=%d users=%d success=%d want=%d", c.total, c.users, success, want)
		}
		if s.left() != c.total-int(want) {
			t.Fatalf("left=%d", s.left())
		}
	}
}

func TestRefundIdempotent(t *testing.T) {
	s := newStock(1)
	if ok, _ := s.deduct("u1"); !ok {
		t.Fatal("want granted")
	}
	if !s.release("u1") {
		t.Fatal("want released")
	}
	if s.release("u1") {
		t.Fatal("duplicate release should fail")
	}
	if s.left() != 1 {
		t.Fatalf("left=%d", s.left())
	}
}

func TestPaidVsReleaseRace(t *testing.T) {
	cases := []struct {
		payFirst bool
		wantLeft int
	}{{true, 0}, {false, 1}}
	for _, c := range cases {
		s := newStock(1)
		_, _ = s.deduct("u1")
		if c.payFirst {
			s.markPaid("u1")
			s.release("u1")
		} else {
			s.release("u1")
			s.markPaid("u1")
		}
		if s.left() != c.wantLeft {
			t.Fatalf("payFirst=%v left=%d want=%d", c.payFirst, s.left(), c.wantLeft)
		}
	}
}

func BenchmarkDeduct(b *testing.B) {
	s := newStock(1 << 30)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = s.deduct("u" + strconv.Itoa(i))
	}
}
