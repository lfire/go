package kv

import "sync"

var _ Storage = (*Memory)(nil)

// Memory 是第 111 讲的原实现：一张 map 加一把读写锁，值用 []byte 承载二进制。
type Memory struct {
	mu     sync.RWMutex
	data   map[string][]byte
	closed bool
}

func NewMemory() *Memory {
	return &Memory{data: make(map[string][]byte)}
}

func (m *Memory) Apply(cmd Command) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	switch cmd.Op {
	case OpGet:
		m.mu.RLock()
		defer m.mu.RUnlock()
		if m.closed {
			return Result{}, ErrClosed
		}
		v, ok := m.data[cmd.Key]
		if !ok {
			return Result{}, ErrNotFound
		}
		return Result{Value: append([]byte(nil), v...), Found: true}, nil
	case OpSet:
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.closed {
			return Result{}, ErrClosed
		}
		m.data[cmd.Key] = append([]byte(nil), cmd.Value...)
		return Result{Found: true}, nil
	case OpDel:
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.closed {
			return Result{}, ErrClosed
		}
		_, ok := m.data[cmd.Key]
		delete(m.data, cmd.Key)
		return Result{Found: ok}, nil
	default:
		return Result{}, ErrBadOp
	}
}

func (m *Memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	return nil
}

func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}
