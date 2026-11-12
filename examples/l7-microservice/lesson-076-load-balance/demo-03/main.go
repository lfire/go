package main

import (
	"fmt"
	"hash/crc32"
	"sort"
)

type Ring struct {
	points []uint32          // 排序后的虚拟节点哈希
	owner  map[uint32]string // 哈希点到真实节点
}

func NewRing(nodes []string, replicas int) *Ring {
	r := &Ring{owner: make(map[uint32]string)}
	for _, n := range nodes {
		for i := 0; i < replicas; i++ {
			key := fmt.Sprintf("%s#%d", n, i)
			h := crc32.ChecksumIEEE([]byte(key))
			r.points = append(r.points, h)
			r.owner[h] = n
		}
	}
	sort.Slice(r.points, func(i, j int) bool { return r.points[i] < r.points[j] })
	return r
}

func (r *Ring) Get(key string) string {
	if len(r.points) == 0 {
		return ""
	}
	h := crc32.ChecksumIEEE([]byte(key))
	// 二分找到第一个不小于 h 的虚拟节点，越过环末尾则回绕到开头
	idx := sort.Search(len(r.points), func(i int) bool { return r.points[i] >= h })
	if idx == len(r.points) {
		idx = 0
	}
	return r.owner[r.points[idx]]
}

func main() {
	ring := NewRing([]string{"node-A", "node-B", "node-C"}, 100)
	for _, k := range []string{"user:1", "user:2", "user:3", "user:1"} {
		fmt.Printf("key=%s 命中=%s\n", k, ring.Get(k))
	}

	// 扩容到 4 个节点，观察迁移量远小于全量
	ring2 := NewRing([]string{"node-A", "node-B", "node-C", "node-D"}, 100)
	moved := 0
	for _, k := range []string{"user:1", "user:2", "user:3"} {
		if ring.Get(k) != ring2.Get(k) {
			moved++
		}
	}
	fmt.Printf("扩容后迁移的 key 数: %d\n", moved)
}
