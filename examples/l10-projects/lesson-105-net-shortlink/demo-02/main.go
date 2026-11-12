package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// Encode62 把非负整数转成 Base62，0 单独处理避免返回空串。
func Encode62(n uint64) string {
	if n == 0 {
		return string(alphabet[0])
	}
	var sb strings.Builder
	for n > 0 {
		sb.WriteByte(alphabet[n%62])
		n /= 62
	}
	b := []byte(sb.String())
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// Decode62 反解短码，遇到字母表外字符返回 false，供入口做白名单校验。
func Decode62(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	var n uint64
	for i := 0; i < len(s); i++ {
		idx := strings.IndexByte(alphabet, s[i])
		if idx < 0 {
			return 0, false
		}
		n = n*62 + uint64(idx)
	}
	return n, true
}

// Allocator 号段式发号器，高水位游标持久化到文件，重启不重复。
type Allocator struct {
	mu   sync.Mutex
	path string
	step uint64
	cur  uint64
	max  uint64
}

// NewAllocator 从游标文件恢复，文件不存在视为从零开始。
func NewAllocator(path string, step uint64) (*Allocator, error) {
	if step == 0 {
		return nil, errors.New("号段步长必须大于零")
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("读取游标失败: %w", err)
	}
	var persisted uint64
	if len(data) > 0 {
		persisted, err = strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("游标内容损坏: %w", err)
		}
	}
	return &Allocator{path: path, step: step, cur: persisted + 1, max: persisted}, nil
}

// Next 领号；号段用尽时先把新高水位原子落盘，再发放本段 ID。
func (a *Allocator) Next() (uint64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cur > a.max {
		newMax := a.max + a.step
		if err := a.persist(newMax); err != nil {
			return 0, err
		}
		a.cur = newMax - a.step + 1
		a.max = newMax
	}
	id := a.cur
	a.cur++
	return id, nil
}

func (a *Allocator) persist(v uint64) error {
	tmp := a.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.FormatUint(v, 10)), 0o644); err != nil {
		return fmt.Errorf("写临时游标失败: %w", err)
	}
	if err := os.Rename(tmp, a.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换游标失败: %w", err)
	}
	return nil
}

func main() {
	dir, err := os.MkdirTemp("", "idgen-")
	if err != nil {
		fmt.Println("建临时目录失败:", err)
		return
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "cursor")

	a, err := NewAllocator(path, 1000)
	if err != nil {
		fmt.Println("初始化发号器失败:", err)
		return
	}
	for i := 0; i < 3; i++ {
		id, err := a.Next()
		if err != nil {
			fmt.Println("发号失败:", err)
			return
		}
		fmt.Printf("id=%d code=%s\n", id, Encode62(id))
	}
	// 模拟进程重启：同一游标文件重新加载，号段跳过未用尽的 ID 但绝不回退。
	b, err := NewAllocator(path, 1000)
	if err != nil {
		fmt.Println("重启恢复失败:", err)
		return
	}
	id, _ := b.Next()
	fmt.Println("重启后首个:", id, Encode62(id))
	if back, ok := Decode62("a!b"); !ok {
		fmt.Println("非法字符被拒, 反解值=", back)
	}
}
