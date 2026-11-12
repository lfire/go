// 文件：hashring.go
package kv

import (
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
)

// VNode 是哈希环上的一个虚拟节点；物理节点靠多个 VNode 打散，避免数据倾斜。
type VNode struct {
	Hash uint32
	Node string
}

// Migration 描述成员变更后需要搬迁的一段哈希区间。
// 取模分片在 N 变化时会让几乎所有 key 迁移，一致性哈希只影响相邻一小段。
type Migration struct {
	From  string
	To    string
	Start uint32
	End   uint32
}

type HashRing struct {
	mu       sync.RWMutex
	replicas int // 每个物理节点的虚拟节点数
	vnodes   []VNode
	nodes    map[string]bool
}

func NewHashRing(replicasPerNode int) *HashRing {
	if replicasPerNode <= 0 {
		replicasPerNode = 128
	}
	return &HashRing{replicas: replicasPerNode, nodes: make(map[string]bool)}
}

func hashKey(key string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return h.Sum32()
}

func vnodeHash(node string, i int) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(fmt.Sprintf("%s#%d", node, i)))
	return h.Sum32()
}

func sortVNodes(v []VNode) {
	sort.Slice(v, func(i, j int) bool {
		if v[i].Hash != v[j].Hash {
			return v[i].Hash < v[j].Hash
		}
		return v[i].Node < v[j].Node
	})
}

func predecessor(vnodes []VNode, h uint32) VNode {
	if len(vnodes) == 0 {
		return VNode{}
	}
	idx := sort.Search(len(vnodes), func(i int) bool { return vnodes[i].Hash >= h })
	if idx == 0 {
		return vnodes[len(vnodes)-1]
	}
	return vnodes[idx-1]
}

func successor(vnodes []VNode, h uint32) VNode {
	if len(vnodes) == 0 {
		return VNode{}
	}
	idx := sort.Search(len(vnodes), func(i int) bool { return vnodes[i].Hash >= h })
	if idx == len(vnodes) {
		return vnodes[0]
	}
	return vnodes[idx]
}

// Add 加入一个物理节点，返回它需要从哪些旧节点接管哪些区间。
// 首个节点不需要迁移；扩容时每个新 VNode 只从环上的后继节点接管一小段。
func (r *HashRing) Add(node string) []Migration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.nodes[node] {
		return nil
	}
	old := append([]VNode(nil), r.vnodes...)
	var added []VNode
	for i := 0; i < r.replicas; i++ {
		added = append(added, VNode{Hash: vnodeHash(node, i), Node: node})
	}
	r.nodes[node] = true
	r.vnodes = append(r.vnodes, added...)
	sortVNodes(r.vnodes)
	if len(old) == 0 {
		return nil
	}
	var out []Migration
	for _, vn := range added {
		pred := predecessor(old, vn.Hash)
		succ := successor(old, vn.Hash)
		if succ.Node == node {
			continue
		}
		out = append(out, Migration{From: succ.Node, To: node, Start: pred.Hash, End: vn.Hash})
	}
	return out
}

// Remove 摘除一个物理节点，返回它的区间由哪些后继节点接管。
func (r *HashRing) Remove(node string) []Migration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.nodes[node] {
		return nil
	}
	old := append([]VNode(nil), r.vnodes...)
	kept := make([]VNode, 0, len(r.vnodes))
	for _, vn := range r.vnodes {
		if vn.Node != node {
			kept = append(kept, vn)
		}
	}
	delete(r.nodes, node)
	r.vnodes = kept
	var out []Migration
	for _, vn := range old {
		if vn.Node != node {
			continue
		}
		pred := predecessor(old, vn.Hash)
		succ := successor(old, vn.Hash)
		if succ.Node == node {
			continue
		}
		out = append(out, Migration{From: node, To: succ.Node, Start: pred.Hash, End: vn.Hash})
	}
	return out
}

// Get 顺时针找到第一个不小于 key 哈希的 VNode，返回其物理节点。
func (r *HashRing) Get(key string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.vnodes) == 0 {
		return ""
	}
	return successor(r.vnodes, hashKey(key)).Node
}

func (r *HashRing) Nodes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.nodes))
	for n := range r.nodes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (r *HashRing) VNodeCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.vnodes)
}
