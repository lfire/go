// 文件：shard.go
package kv

import "sync"

const shardCount = 32

type shard struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// ShardTable 把一张逻辑表拆成固定数量的分片，锁粒度从「整表」降到「单个哈希桶」。
// 32 是 2 的幂，取模可直接用位与，在 key 分布均匀时写竞争摊到 32 把锁上。
type ShardTable struct {
	shards [shardCount]shard
}

func NewShardTable() *ShardTable {
	t := &ShardTable{}
	for i := range t.shards {
		t.shards[i].data = make(map[string][]byte)
	}
	return t
}

// shardOf 用 FNV-1a 把 key 摊到分片；手写循环避免每次调用分配 hasher。
func shardOf(key string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return h % shardCount
}

func (t *ShardTable) of(key string) *shard {
	return &t.shards[shardOf(key)]
}

// Get 返回副本，避免调用方持有内部切片后并发改写造成数据竞争。
func (t *ShardTable) Get(key string) ([]byte, bool) {
	sh := t.of(key)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	v, ok := sh.data[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), v...), true
}

func (t *ShardTable) Set(key string, val []byte) {
	sh := t.of(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.data[key] = append([]byte(nil), val...)
}

func (t *ShardTable) Del(key string) bool {
	sh := t.of(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	_, ok := sh.data[key]
	delete(sh.data, key)
	return ok
}

// Apply 只处理写命令，供写路径与重放路径共用；天然幂等。
func (t *ShardTable) Apply(cmd Command) {
	switch cmd.Op {
	case OpSet:
		t.Set(cmd.Key, cmd.Value)
	case OpDel:
		t.Del(cmd.Key)
	}
}

// Snapshot 在统一锁序下拷贝全量数据，供快照器生成一致视图。
func (t *ShardTable) Snapshot() map[string][]byte {
	all := make(map[string][]byte)
	for i := range t.shards {
		sh := &t.shards[i]
		sh.mu.RLock()
		for k, v := range sh.data {
			all[k] = append([]byte(nil), v...)
		}
		sh.mu.RUnlock()
	}
	return all
}

func (t *ShardTable) Len() int {
	n := 0
	for i := range t.shards {
		sh := &t.shards[i]
		sh.mu.RLock()
		n += len(sh.data)
		sh.mu.RUnlock()
	}
	return n
}
