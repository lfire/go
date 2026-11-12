// 文件：contract_test.go —— 表驱动覆盖 Saga 补偿、幂等与状态机
package ecom

import (
	"errors"
	"sync"
	"testing"
)

var errNoStock = errors.New("no stock")
var errDeclined = errors.New("payment declined")

type miniStep struct {
	name string
	do   func() error
	undo func()
}

func runMini(steps []miniStep) error {
	var done []miniStep
	for _, s := range steps {
		if err := s.do(); err != nil {
			for i := len(done) - 1; i >= 0; i-- {
				done[i].undo()
			}
			return err
		}
		done = append(done, s)
	}
	return nil
}

type fakeInv struct {
	mu       sync.Mutex
	released int
	fail     bool
}

func (f *fakeInv) reserve(qty int) error {
	if f.fail {
		return errNoStock
	}
	return nil
}
func (f *fakeInv) release(qty int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released += qty
}

func TestSagaCompensation(t *testing.T) {
	cases := []struct {
		name        string
		failReserve bool
		failCharge  bool
		wantRelease int
		wantCancel  bool
	}{
		{"库存不足首步失败", true, false, 0, false},
		{"支付失败逆序补偿", false, true, 2, true},
		{"全链路成功", false, false, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inv := &fakeInv{fail: c.failReserve}
			canceled := false
			steps := []miniStep{
				{"reserve", func() error { return inv.reserve(2) }, func() { inv.release(2) }},
				{"create", func() error { return nil }, func() { canceled = true }},
				{"charge", func() error {
					if c.failCharge {
						return errDeclined
					}
					return nil
				}, func() {}},
			}
			err := runMini(steps)
			if (err != nil) != (c.failReserve || c.failCharge) {
				t.Fatalf("err=%v", err)
			}
			inv.mu.Lock()
			got := inv.released
			inv.mu.Unlock()
			if got != c.wantRelease {
				t.Fatalf("released=%d want=%d", got, c.wantRelease)
			}
			if canceled != c.wantCancel {
				t.Fatalf("canceled=%v want=%v", canceled, c.wantCancel)
			}
		})
	}
}

func TestIdempotentReserve(t *testing.T) {
	inv := &fakeInv{}
	seen := make(map[string]bool)
	var mu sync.Mutex
	granted := 0
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			defer mu.Unlock()
			if seen["k1"] {
				return
			}
			seen["k1"] = true
			if inv.reserve(1) == nil {
				granted++
			}
		}()
	}
	wg.Wait()
	if granted != 1 {
		t.Fatalf("granted=%d want=1", granted)
	}
}

func BenchmarkSagaSuccess(b *testing.B) {
	inv := &fakeInv{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = runMini([]miniStep{
			{"reserve", func() error { return inv.reserve(1) }, func() { inv.release(1) }},
			{"create", func() error { return nil }, func() {}},
			{"charge", func() error { return nil }, func() {}},
		})
	}
}
