// 文件：wal_storage.go
package kv

import (
	"errors"
	"sync"
	"time"
)

var _ Storage = (*WALStorage)(nil)

// WALStorage 是第 113 讲的持久化实现：写先经 WAL 追加，再落到分片表。
// 外层 mu 把「Append + Apply」串成原子步骤，保证日志顺序与状态顺序一致；
// 读路径绕开它，只取目标分片的读锁。
type WALStorage struct {
	mu      sync.Mutex
	table   *ShardTable
	wal     *WAL
	metrics *Metrics
	closed  bool
}

type WALOptions struct {
	Dir         string
	SyncPolicy  SyncPolicy
	GroupWindow time.Duration
	SegmentSize int64
	Snapshot    string
}

// OpenWALStorage 的恢复顺序固定为：加载快照 → 从位点重放 → 裁剪脏尾巴 → 开新段。
// 四步缺一不可，尤其第三步决定了重启后能否继续安全追加。
func OpenWALStorage(opts WALOptions) (*WALStorage, error) {
	m := &Metrics{}
	table := NewShardTable()
	from := int64(0)
	if opts.Snapshot != "" {
		if off, data, err := LoadSnapshot(opts.Snapshot); err == nil {
			for k, v := range data {
				table.Set(k, v)
			}
			from = off
		}
	}
	lastGood, err := Replay(opts.Dir, from, m, func(cmd Command) error {
		table.Apply(cmd)
		m.replayRecords.Add(1)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := TruncateTo(opts.Dir, lastGood); err != nil {
		return nil, err
	}
	wal, err := OpenWAL(opts.Dir, opts.SyncPolicy, opts.GroupWindow, opts.SegmentSize, m)
	if err != nil {
		return nil, err
	}
	return &WALStorage{table: table, wal: wal, metrics: m}, nil
}

func (s *WALStorage) Apply(cmd Command) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	if cmd.Op == OpGet {
		s.metrics.reads.Add(1)
		v, ok := s.table.Get(cmd.Key)
		if !ok {
			return Result{}, ErrNotFound
		}
		return Result{Value: v, Found: true}, nil
	}
	if cmd.Op != OpSet && cmd.Op != OpDel {
		return Result{}, ErrBadOp
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Result{}, ErrClosed
	}
	// 顺序不可颠倒：日志写成功才改内存，否则已确认的写会在崩溃后丢失。
	if _, err := s.wal.Append(cmd); err != nil {
		if errors.Is(err, ErrNoSpace) {
			s.metrics.diskFull.Add(1)
		}
		if errors.Is(err, ErrPartialWrite) {
			s.metrics.partialWrites.Add(1)
		}
		return Result{}, err
	}
	s.table.Apply(cmd)
	s.metrics.writes.Add(1)
	return Result{Found: true}, nil
}

func (s *WALStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.wal.Close()
}

// Metrics 暴露只读指标视图，供第 9 节的 /metrics 端点抓取。
func (s *WALStorage) Metrics() map[string]int64 {
	return s.metrics.Read()
}
