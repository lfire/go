// 文件：wal.go
package kv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// SyncPolicy 决定 Append 何时真正把数据交给磁盘，是吞吐与 RPO 的核心旋钮。
type SyncPolicy uint8

const (
	SyncEvery SyncPolicy = iota // 每条 fsync，RPO≈0，吞吐最低
	SyncGroup                   // 后台按窗口批量 fsync，RPO=窗口
	SyncNone                    // 只 write 不 fsync，仅用于可丢数据场景
)

// WAL 只做一件事：把命令以「长度 + CRC + 载荷」追加到当前段。
// mu 同时保护文件句柄、位点与组提交计数；Append 是唯一写入口。
type WAL struct {
	mu          sync.Mutex
	dir         string
	f           *os.File
	base        int64 // 之前所有段的累计字节
	offset      int64 // 当前段内已写字节
	segIndex    int
	segmentSize int64
	policy      SyncPolicy
	window      time.Duration
	pending     int
	stop        chan struct{}
	wg          sync.WaitGroup
	closed      bool
	metrics     *Metrics
}

// OpenWAL 总是新建一个段：旧段保持只读，崩溃尾巴不会与新写混在一起。
// base 由当前所有段的大小累加得到，从而给出连续的全局位点。
func OpenWAL(dir string, policy SyncPolicy, window time.Duration, segSize int64, m *Metrics) (*WAL, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if segSize <= 0 {
		segSize = 64 << 20
	}
	if window <= 0 {
		window = 5 * time.Millisecond
	}
	segs, err := listSegments(dir)
	if err != nil {
		return nil, err
	}
	idx := 0
	var base int64
	for _, s := range segs {
		idx = s.index + 1
		base += s.size
	}
	name := filepath.Join(dir, fmt.Sprintf("wal-%06d.log", idx))
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	w := &WAL{
		dir: dir, f: f, base: base, segIndex: idx,
		segmentSize: segSize, policy: policy, window: window,
		stop: make(chan struct{}), metrics: m,
	}
	if policy == SyncGroup {
		w.wg.Add(1)
		go w.syncLoop()
	}
	return w, nil
}

// Append 追加一条写命令，返回写入后的全局位点。
// 三处边界判断是重点：段轮转、磁盘满、部分写。
func (w *WAL) Append(cmd Command) (int64, error) {
	rec, err := encodeRecord(cmd)
	if err != nil {
		return 0, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.f == nil {
		return 0, ErrClosed
	}
	if w.offset+int64(len(rec)) > w.segmentSize {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(rec)
	if err != nil {
		if errors.Is(err, syscall.ENOSPC) {
			return 0, fmt.Errorf("%w: %v", ErrNoSpace, err)
		}
		return 0, err
	}
	if n != len(rec) {
		// 部分写：把文件裁回写入前的边界，绝不让半条记录留在日志尾。
		if terr := w.f.Truncate(w.offset); terr != nil {
			return 0, fmt.Errorf("%w: rollback: %v", ErrPartialWrite, terr)
		}
		return 0, ErrPartialWrite
	}
	w.offset += int64(n)
	if w.policy == SyncGroup {
		w.pending++
	}
	if w.policy == SyncEvery {
		if err := w.f.Sync(); err != nil {
			return 0, err
		}
		if w.metrics != nil {
			w.metrics.syncCount.Add(1)
		}
	}
	if w.metrics != nil {
		w.metrics.walBytes.Add(int64(n))
	}
	return w.base + w.offset, nil
}

func (w *WAL) rotateLocked() error {
	if err := w.f.Sync(); err != nil {
		return err
	}
	if err := w.f.Close(); err != nil {
		return err
	}
	w.base += w.offset
	w.offset = 0
	w.segIndex++
	name := filepath.Join(w.dir, fmt.Sprintf("wal-%06d.log", w.segIndex))
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	w.f = f
	return nil
}

func (w *WAL) syncLoop() {
	defer w.wg.Done()
	ticker := time.NewTicker(w.window)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.mu.Lock()
			if !w.closed && w.f != nil && w.pending > 0 {
				if err := w.f.Sync(); err == nil {
					w.pending = 0
					if w.metrics != nil {
						w.metrics.syncCount.Add(1)
					}
				}
			}
			w.mu.Unlock()
		case <-w.stop:
			w.mu.Lock()
			if w.f != nil {
				_ = w.f.Sync()
				if w.metrics != nil {
					w.metrics.syncCount.Add(1)
				}
			}
			w.mu.Unlock()
			return
		}
	}
}

// Offset 返回当前全局位点，供快照记录「日志读到这里」。
func (w *WAL) Offset() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.base + w.offset
}

func (w *WAL) Close() error {
	if w.policy == SyncGroup {
		close(w.stop)
		w.wg.Wait()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.f == nil {
		return nil
	}
	if err := w.f.Sync(); err != nil {
		_ = w.f.Close()
		return err
	}
	return w.f.Close()
}
