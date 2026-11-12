package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Task struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"created_at"`
}

type FileV2 struct {
	Version int    `json:"version"`
	Tasks   []Task `json:"tasks"`
}

// migrateV1 把旧版裸数组升级为带版本的包络。
func migrateV1(raw []byte) (*FileV2, error) {
	var old []Task
	if err := json.Unmarshal(raw, &old); err != nil {
		return nil, fmt.Errorf("v1 解析失败: %w", err)
	}
	return &FileV2{Version: 2, Tasks: old}, nil
}

// load 按 version 选择解析路径，兼容 v1 与 v2 两种文件。
func load(path string) (*FileV2, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var probe struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(b, &probe); err == nil && probe.Version >= 2 {
		var f FileV2
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, fmt.Errorf("v2 解析失败: %w", err)
		}
		return &f, nil
	}
	return migrateV1(b)
}

// archive 把超过 TTL 且已完成的任务移出主文件，返回归档条数。
func archive(f *FileV2, ttl time.Duration, now time.Time) int {
	kept := make([]Task, 0, len(f.Tasks))
	moved := 0
	for _, t := range f.Tasks {
		if t.Done && now.Sub(t.CreatedAt) > ttl {
			moved++
			continue
		}
		kept = append(kept, t)
	}
	f.Tasks = kept
	return moved
}

// backup 写一份带时间戳的快照，供损坏时回滚。
func backup(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("todo-%s.json.bak", time.Now().Format("20060102-150405"))
	return os.WriteFile(filepath.Join(filepath.Dir(path), name), b, 0o600)
}

func main() {
	dir, _ := os.MkdirTemp("", "todo-")
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "todo.json")
	raw := `[{"id":1,"title":"旧任务","done":true,"created_at":"2020-01-01T00:00:00Z"}]`
	if err := os.WriteFile(p, []byte(raw), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	if err := backup(p); err != nil {
		fmt.Fprintln(os.Stderr, "备份失败:", err)
		return
	}
	f, err := load(p)
	if err != nil {
		fmt.Fprintln(os.Stderr, "加载失败:", err)
		return
	}
	n := archive(f, 720*time.Hour, time.Now())
	fmt.Printf("迁移后 version=%d 归档=%d 剩余=%d\n", f.Version, n, len(f.Tasks))
}
