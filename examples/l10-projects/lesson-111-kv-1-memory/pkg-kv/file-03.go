package kv

import (
	"hash/fnv"
	"sync"
)

const DefaultShards = 32

var _ Storage = (*ShardedMemory)(nil)

type shard struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// ShardedMemory 把一张大表拆成 N 个独立加锁的分片，写竞争随之摊开。
// 它实现同一个 Storage，读多写少或高并发写时可无缝替换 Memory。
type ShardedMemory struct {
	shards []shard
	closed bool
	mu     sync.RWMutex // 仅保护 closed 标志，不与分片锁嵌套
}

func NewShardedMemory(n int) *ShardedMemory {
	if n <= 0 {
		n = DefaultShards
	}
	s := &ShardedMemory{shards: make([]shard, n)}
	for i := range s.shards {
		s.shards[i].data = make(map[string][]byte)
	}
	return s
}

func (s *ShardedMemory) index(key string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % uint32(len(s.shards)))
}

func (s *ShardedMemory) Apply(cmd Command) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	if len(s.shards) == 0 {
		return Result{}, ErrClosed
	}
	s.mu.RLock()
	closed := s.closed
	s.mu.RUnlock()
	if closed {
		return Result{}, ErrClosed
	}
	sh := &s.shards[s.index(cmd.Key)]
	switch cmd.Op {
	case OpGet:
		sh.mu.RLock()
		defer sh.mu.RUnlock()
		v, ok := sh.data[cmd.Key]
		if !ok {
			return Result{}, ErrNotFound
		}
		return Result{Value: append([]byte(nil), v...), Found: true}, nil
	case OpSet:
		sh.mu.Lock()
		defer sh.mu.Unlock()
		sh.data[cmd.Key] = append([]byte(nil), cmd.Value...)
		return Result{Found: true}, nil
	case OpDel:
		sh.mu.Lock()
		defer sh.mu.Unlock()
		_, ok := sh.data[cmd.Key]
		delete(sh.data, cmd.Key)
		return Result{Found: ok}, nil
	default:
		return Result{}, ErrBadOp
	}
}

func (s *ShardedMemory) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return nil
}
