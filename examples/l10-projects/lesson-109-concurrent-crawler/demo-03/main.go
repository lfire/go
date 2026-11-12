package main

import (
	"fmt"
	"hash/fnv"
	"sync"
)

// Deduplicator 是去重层抽象：Visit 返回 true 表示首次出现、应调度。
type Deduplicator interface {
	Visit(url string) bool
}

// BloomFilter 是固定容量布隆过滤器：位图加 k 个哈希。
type BloomFilter struct {
	bits []uint64
	m    uint64
	k    uint64
}

func NewBloomFilter(m, k uint64) *BloomFilter {
	return &BloomFilter{bits: make([]uint64, (m+63)/64), m: m, k: k}
}

func (b *BloomFilter) hashes(s string) (uint64, uint64) {
	h1 := fnv.New64a()
	_, _ = h1.Write([]byte(s))
	a := h1.Sum64()
	h2 := fnv.New64()
	_, _ = h2.Write([]byte(s))
	c := h2.Sum64() | 1 // 保证步进为奇数，减少退化
	return a, c
}

func (b *BloomFilter) Add(s string) {
	a, c := b.hashes(s)
	for i := uint64(0); i < b.k; i++ {
		idx := (a + c*i) % b.m
		b.bits[idx/64] |= 1 << (idx % 64)
	}
}

func (b *BloomFilter) MightContain(s string) bool {
	a, c := b.hashes(s)
	for i := uint64(0); i < b.k; i++ {
		idx := (a + c*i) % b.m
		if b.bits[idx/64]&(1<<(idx%64)) == 0 {
			return false
		}
	}
	return true
}

// URLDedup 组合布隆快判与精确 set，用一把锁包住整个 Visit。
type URLDedup struct {
	mu    sync.Mutex
	bloom *BloomFilter
	exact map[string]struct{}
}

func NewURLDedup() *URLDedup {
	return &URLDedup{bloom: NewBloomFilter(1<<20, 4), exact: map[string]struct{}{}}
}

// Visit 是唯一入口：判定与标记在同一临界区，避免重复入队。
func (d *URLDedup) Visit(s string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.bloom.MightContain(s) {
		if _, ok := d.exact[s]; ok {
			return false
		}
	}
	d.bloom.Add(s)
	d.exact[s] = struct{}{}
	return true
}

var _ Deduplicator = (*URLDedup)(nil)

func main() {
	d := NewURLDedup()
	for _, u := range []string{"/a", "/b", "/a", "/c", "/b"} {
		if d.Visit(u) {
			fmt.Println("首次调度:", u)
		} else {
			fmt.Println("重复丢弃:", u)
		}
	}
}
