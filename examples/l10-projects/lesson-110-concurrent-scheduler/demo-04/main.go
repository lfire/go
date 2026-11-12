package main

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
)

// Ring 是一致性哈希环：虚拟节点分散数据，扩容只迁移相邻区间。
type Ring struct {
	tokens []uint64
	nodes  map[uint64]string
	vnode  int
}

func NewRing(vnode int) *Ring {
	return &Ring{nodes: map[uint64]string{}, vnode: vnode}
}

func h(s string) uint64 {
	f := fnv.New64a()
	_, _ = f.Write([]byte(s))
	return f.Sum64()
}

// Add 为物理节点生成 vnode 个虚拟节点落到环上。
func (r *Ring) Add(node string) {
	for i := 0; i < r.vnode; i++ {
		t := h(node + "#" + strconv.Itoa(i))
		r.tokens = append(r.tokens, t)
		r.nodes[t] = node
	}
	sort.Slice(r.tokens, func(i, j int) bool { return r.tokens[i] < r.tokens[j] })
}

// Owner 顺时针找第一个不小于 key 哈希的虚拟节点。
func (r *Ring) Owner(key string) string {
	if len(r.tokens) == 0 {
		return ""
	}
	t := h(key)
	idx := sort.Search(len(r.tokens), func(i int) bool { return r.tokens[i] >= t })
	if idx == len(r.tokens) {
		idx = 0 // 回绕到环首
	}
	return r.nodes[r.tokens[idx]]
}

func main() {
	r := NewRing(128)
	r.Add("worker-1")
	r.Add("worker-2")
	r.Add("worker-3")
	keys := []string{"report-daily", "cleanup", "heartbeat", "sync", "settle"}
	owner3 := map[string]string{}
	for _, k := range keys {
		owner3[k] = r.Owner(k)
	}
	before := map[string]int{}
	for _, o := range owner3 {
		before[o]++
	}
	fmt.Println("3 节点分布:", before)

	r4 := NewRing(128)
	for _, n := range []string{"worker-1", "worker-2", "worker-3", "worker-4"} {
		r4.Add(n)
	}
	moved := 0
	for _, k := range keys {
		if r4.Owner(k) != owner3[k] {
			moved++
		}
	}
	fmt.Printf("扩容后迁移 %d/%d 个 key（只动相邻区间）\n", moved, len(keys))
}
