package main

import (
	"fmt"
	"hash/fnv"
	"math"
	"sync"
)

// Bloom 是并发安全的位数组布隆过滤器，双哈希模拟 k 个哈希函数。
type Bloom struct {
	mu   sync.RWMutex
	bits []uint64
	m    uint64
	k    uint64
}

func NewBloom(n int, fp float64) *Bloom {
	if n <= 0 {
		n = 1
	}
	if fp <= 0 || fp >= 1 {
		fp = 0.001
	}
	m := uint64(math.Ceil(-float64(n) * math.Log(fp) / (math.Ln2 * math.Ln2)))
	if m == 0 {
		m = 64
	}
	k := uint64(math.Round(float64(m) / float64(n) * math.Ln2))
	if k == 0 {
		k = 1
	}
	return &Bloom{bits: make([]uint64, (m+63)/64), m: m, k: k}
}

func (b *Bloom) hashes(s string) (uint64, uint64) {
	h1 := fnv.New64a()
	_, _ = h1.Write([]byte(s))
	h2 := fnv.New64()
	_, _ = h2.Write([]byte(s))
	return h1.Sum64(), h2.Sum64()
}

func (b *Bloom) Add(s string) {
	a, c := b.hashes(s)
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := uint64(0); i < b.k; i++ {
		idx := (a + c*i) % b.m
		b.bits[idx/64] |= 1 << (idx % 64)
	}
}

func (b *Bloom) Contains(s string) bool {
	a, c := b.hashes(s)
	b.mu.RLock()
	defer b.mu.RUnlock()
	for i := uint64(0); i < b.k; i++ {
		idx := (a + c*i) % b.m
		if b.bits[idx/64]&(1<<(idx%64)) == 0 {
			return false
		}
	}
	return true
}

func main() {
	b := NewBloom(10000, 0.001)
	for i := 0; i < 1000; i++ {
		b.Add(fmt.Sprintf("code-%d", i))
	}
	hit := b.Contains("code-500")
	fmt.Println("已存在短码判存在:", hit)
	miss := b.Contains("code-999999")
	fmt.Println("不存在短码判存在:", miss)
	fp := 0
	for i := 0; i < 100000; i++ {
		if b.Contains(fmt.Sprintf("absent-%d", i)) {
			fp++
		}
	}
	fmt.Printf("十万次不存在查询误判=%d 误判率=%.4f\n", fp, float64(fp)/100000)
}
