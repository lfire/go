package main

import (
	"fmt"
	"hash/fnv"
	"sync"
)

// ShardMap 用锁分片降低热点竞争：按 key 哈希把一把大锁拆成 32 把。
type shard struct {
	mu sync.RWMutex
	m  map[string]string
}

type ShardMap struct {
	shards []shard
}

func NewShardMap(n int) *ShardMap {
	s := &ShardMap{shards: make([]shard, n)}
	for i := range s.shards {
		s.shards[i].m = make(map[string]string)
	}
	return s
}

func (s *ShardMap) idx(key string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return h.Sum32() % uint32(len(s.shards))
}

func (s *ShardMap) Set(key, val string) {
	sh := &s.shards[s.idx(key)]
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.m[key] = val
}

func (s *ShardMap) Get(key string) (string, bool) {
	sh := &s.shards[s.idx(key)]
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	v, ok := sh.m[key]
	return v, ok
}

func main() {
	m := NewShardMap(32)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("code-%d", i)
			m.Set(key, "target")
		}(i)
	}
	wg.Wait()
	v, ok := m.Get("code-42")
	fmt.Println("分片读取:", v, ok)
	fmt.Println("分片数:", len(m.shards))
}
