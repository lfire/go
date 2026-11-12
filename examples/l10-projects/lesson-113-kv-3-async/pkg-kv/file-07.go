// 文件：snapshot.go
package kv

import (
	"encoding/gob"
	"fmt"
	"os"
)

// snapshotFile 是快照的磁盘格式：位点 + 全量键值。
// 位点记录生成快照时 WAL 的全局字节偏移，恢复时只重放其后的记录。
type snapshotFile struct {
	Offset int64
	Data   map[string][]byte
}

// Snapshot 生成一致视图并原子落盘：先写 tmp，Sync，再 Rename。
// 全程持有 s.mu，保证位点与数据来自同一时刻、且期间没有新写插入。
func (s *WALStorage) Snapshot(path string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosed
	}
	off := s.wal.Offset()
	snap := snapshotFile{Offset: off, Data: s.table.Snapshot()}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	if err := gob.NewEncoder(f).Encode(snap); err != nil {
		_ = f.Close()
		return 0, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	// Rename 是同一文件系统内的原子操作：要么看到旧快照，要么看到完整新快照。
	if err := os.Rename(tmp, path); err != nil {
		return 0, err
	}
	s.metrics.compactions.Add(1)
	return off, nil
}

// LoadSnapshot 读取快照；文件不存在或半份快照都返回错误，
// 由调用方退回「从零重放全部日志」这条安全降级路径。
func LoadSnapshot(path string) (int64, map[string][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()
	var snap snapshotFile
	if err := gob.NewDecoder(f).Decode(&snap); err != nil {
		return 0, nil, fmt.Errorf("kv: decode snapshot: %w", err)
	}
	if snap.Data == nil {
		snap.Data = make(map[string][]byte)
	}
	return snap.Offset, snap.Data, nil
}

// Compact 删除位点之前已被快照完全覆盖的旧段，只保留最后一段供继续追加。
func Compact(dir string, safeOffset int64) (int, error) {
	segs, err := listSegments(dir)
	if err != nil {
		return 0, err
	}
	if len(segs) == 0 {
		return 0, nil
	}
	last := segs[len(segs)-1].index
	var covered int64
	removed := 0
	for _, s := range segs {
		if covered+s.size <= safeOffset && s.index < last {
			if err := os.Remove(s.path); err != nil {
				return removed, err
			}
			removed++
		}
		covered += s.size
	}
	return removed, nil
}
