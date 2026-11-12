package main

import (
	"errors"
	"fmt"
	"time"
)

// Priority 表示任务优先级，数值越小越紧急。
type Priority int

const (
	P0 Priority = iota
	P1
	P2
)

// Task 是唯一的领域实体；CreatedAt 用于 TTL 归档，DoneAt 标记完成时刻。
type Task struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Priority  Priority  `json:"priority"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"created_at"`
	DoneAt    time.Time `json:"done_at,omitempty"`
}

// schemaVersion 是文件格式版本，迁移时按它选择升级路径。
const schemaVersion = 2

// File 是落盘包络：带版本与生成时刻，避免裸数组无法演进。
type File struct {
	Version   int    `json:"version"`
	UpdatedAt string `json:"updated_at"`
	Tasks     []Task `json:"tasks"`
}

// Store 抽象存储，命令层只依赖这个接口，方便替换 JSON 或 SQLite。
type Store interface {
	List() ([]Task, error)
	Add(title string, p Priority) (Task, error)
	Done(id int) (Task, error)
	Delete(id int) error
	Archive(ttl time.Duration) (int, error)
}

var ErrUsage = errors.New("参数错误")
var ErrNotFound = errors.New("任务不存在")
var ErrCorrupted = errors.New("数据文件损坏")
var ErrBusy = errors.New("存储被其他进程占用")
var ErrLockTimeout = errors.New("获取文件锁超时")

func main() {
	f := File{Version: schemaVersion, Tasks: []Task{{ID: 1, Title: "示例"}}}
	fmt.Printf("version=%d tasks=%d\n", f.Version, len(f.Tasks))
}
