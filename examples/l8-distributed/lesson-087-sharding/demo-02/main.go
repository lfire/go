package main

import (
	"fmt"
	"hash/crc32"
	"sort"
	"strconv"
)

// Ring 是一致性哈希环，virtual 表示每个物理节点的虚拟节点数
type Ring struct {
	virtual int
	points  []uint32
	owner   map[uint32]string
}

func NewRing(virtual int) *Ring {
	return &Ring{virtual: virtual, owner: make(map[uint32]string)}
}

func hash(s string) uint32 {
	return crc32.ChecksumIEEE([]byte(s))
}

// Add 把一个物理节点映射成 virtual 个虚拟点挂到环上
func (r *Ring) Add(node string) {
	for i := 0; i < r.virtual; i++ {
		p := hash(node + "#" + strconv.Itoa(i))
		r.points = append(r.points, p)
		r.owner[p] = node
	}
	sort.Slice(r.points, func(i, j int) bool { return r.points[i] < r.points[j] })
}

// Get 顺时针找到第一个不小于 key 哈希的虚拟点，命中其物理节点
func (r *Ring) Get(key string) string {
	if len(r.points) == 0 {
		return ""
	}
	h := hash(key)
	idx := sort.Search(len(r.points), func(i int) bool { return r.points[i] >= h })
	if idx == len(r.points) {
		idx = 0 // 绕回环的起点
	}
	return r.owner[r.points[idx]]
}

func main() {
	r := NewRing(100) // 每个物理节点 100 个虚拟点
	r.Add("node-A")
	r.Add("node-B")
	r.Add("node-C")

	const total = 10000
	before := make(map[string]string, total)
	keys := make([]string, total)
	for i := range keys {
		keys[i] = "key-" + strconv.Itoa(i)
		before[keys[i]] = r.Get(keys[i])
	}
	r.Add("node-D") // 新增一台，只应迁移落在 D 负责区间上的 key
	moved := 0
	for _, k := range keys {
		if r.Get(k) != before[k] {
			moved++
		}
	}
	fmt.Printf("新增 node-D 后迁移 %d 个 key\n", moved)
	fmt.Println("迁移比例低于取模扩容:", float64(moved)/float64(total) < 0.5)
}
