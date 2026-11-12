package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Task struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type File struct {
	Version int    `json:"version"`
	Tasks   []Task `json:"tasks"`
}

// atomicWrite 把 f 序列化后经临时文件原子替换到 path，全程 fsync 保证落盘。
func atomicWrite(path string, f File) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".todo-*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("写临时文件: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("fsync 临时文件: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("原子替换: %w", err)
	}
	// 目录项也要 fsync，否则断电后可能文件内容在但名字没落盘。
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("打开目录: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("fsync 目录: %w", err)
	}
	return nil
}

func main() {
	dir, _ := os.MkdirTemp("", "todo-")
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "todo.json")
	f := File{Version: 2, Tasks: []Task{{ID: 1, Title: "写第 103 讲"}}}
	if err := atomicWrite(p, f); err != nil {
		fmt.Fprintln(os.Stderr, "写失败:", err)
		return
	}
	fmt.Println("已原子写入", len(f.Tasks), "条任务", time.Now().Format("15:04:05"))
}
