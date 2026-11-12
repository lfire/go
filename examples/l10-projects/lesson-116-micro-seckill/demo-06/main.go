// 文件：segmented.go —— 分段库存把单 key 热点摊到 N 把锁
package main

import (
	"fmt"
	"sync"
)

// Segment 是一段库存，各自一把锁，互不阻塞。
type Segment struct {
	mu   sync.Mutex
	left int
}

// SegmentedStock 把总量切成 N 段，请求按用户 ID 哈希落到某一段，
// 命中不同段的扣减可以真正并行，缓解单 key 串行瓶颈。
type SegmentedStock struct {
	segs []*Segment
	n    int
}

func NewSegmentedStock(total, n int) *SegmentedStock {
	s := &SegmentedStock{segs: make([]*Segment, n), n: n}
	base, extra := total/n, total%n
	for i := 0; i < n; i++ {
		q := base
		if i < extra {
			q++
		}
		s.segs[i] = &Segment{left: q}
	}
	return s
}

// Deduct 先按哈希定位主段，不足时顺序探测其它段，兼顾并行与碎片回收。
func (s *SegmentedStock) Deduct(userID string) bool {
	start := int(hash(userID) % uint64(s.n))
	for k := 0; k < s.n; k++ {
		seg := s.segs[(start+k)%s.n]
		seg.mu.Lock()
		if seg.left > 0 {
			seg.left--
			seg.mu.Unlock()
			return true
		}
		seg.mu.Unlock()
	}
	return false
}

func hash(s string) uint64 {
	var h uint64 = 1469598103934665603
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

func (s *SegmentedStock) Left() int {
	total := 0
	for _, seg := range s.segs {
		seg.mu.Lock()
		total += seg.left
		seg.mu.Unlock()
	}
	return total
}

func main() {
	st := NewSegmentedStock(100, 10)
	var wg sync.WaitGroup
	var mu sync.Mutex
	granted := 0
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if st.Deduct(fmt.Sprintf("u-%d", id)) {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	fmt.Printf("granted=%d left=%d want=100/0\n", granted, st.Left())
}
