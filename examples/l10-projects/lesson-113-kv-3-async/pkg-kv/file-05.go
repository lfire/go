// 文件：recovery.go
package kv

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// segmentInfo 描述一个日志段；index 决定重放顺序，size 参与全局位点计算。
type segmentInfo struct {
	path  string
	index int
	size  int64
}

// listSegments 只认 wal-<序号>.log，忽略 snapshot.gob、*.tmp 等其它文件。
func listSegments(dir string) ([]segmentInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []segmentInfo
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "wal-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "wal-"), ".log"))
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, segmentInfo{
			path: filepath.Join(dir, name), index: idx, size: info.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].index < out[j].index })
	return out, nil
}

// Replay 顺序读取所有段，只把位点不早于 fromOffset 的记录交给 apply。
// 返回已安全重放到的全局位点；遇到半条或 CRC 失败即停止，绝不把脏数据带进状态机。
func Replay(dir string, fromOffset int64, m *Metrics, apply func(Command) error) (int64, error) {
	segs, err := listSegments(dir)
	if err != nil {
		return 0, err
	}
	pos := int64(0)
	for _, s := range segs {
		f, err := os.Open(s.path)
		if err != nil {
			return pos, err
		}
		next, clean, err := replayOne(f, pos, fromOffset, m, apply)
		_ = f.Close()
		pos = next
		if err != nil {
			return pos, err
		}
		if !clean {
			// 崩溃点：段轮转保证脏尾巴只可能在最后一段，此处停止即安全。
			return pos, nil
		}
	}
	return pos, nil
}

func replayOne(f *os.File, start, fromOffset int64, m *Metrics, apply func(Command) error) (int64, bool, error) {
	pos := start
	for {
		head := make([]byte, recordHead)
		if _, err := io.ReadFull(f, head); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return pos, true, nil // 干净段尾
			}
			return pos, false, err
		}
		n := int64(binary.BigEndian.Uint32(head[0:4]))
		if n > maxRecord {
			if m != nil {
				m.crcFailures.Add(1)
			}
			return pos, false, nil
		}
		rec := make([]byte, int(recordHead+n))
		copy(rec, head)
		if _, err := io.ReadFull(f, rec[recordHead:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return pos, false, nil // 半条记录，崩溃点
			}
			return pos, false, err
		}
		cmd, err := decodeRecord(rec)
		if err != nil {
			if m != nil {
				m.crcFailures.Add(1)
			}
			return pos, false, nil
		}
		if pos >= fromOffset {
			if err := apply(cmd); err != nil {
				return pos, false, err
			}
		}
		pos += recordHead + n
	}
}

// TruncateTo 把日志目录裁剪到干净位点，丢弃崩溃留下的半条或损坏尾巴。
// 不清理的话，下次 Append 会追加到脏尾巴之后，恢复将永远卡在同一处。
func TruncateTo(dir string, lastGood int64) error {
	segs, err := listSegments(dir)
	if err != nil {
		return err
	}
	var covered int64
	for i, s := range segs {
		if covered >= lastGood {
			if err := os.Remove(s.path); err != nil {
				return err
			}
			continue
		}
		if covered+s.size > lastGood {
			if err := os.Truncate(s.path, lastGood-covered); err != nil {
				return err
			}
			for j := i + 1; j < len(segs); j++ {
				if err := os.Remove(segs[j].path); err != nil {
					return err
				}
			}
			return nil
		}
		covered += s.size
	}
	return nil
}
