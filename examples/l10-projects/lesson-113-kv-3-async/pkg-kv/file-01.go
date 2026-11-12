// 文件：contract.go
package kv

import "errors"

// Op 用显式数值定义，第 112 讲把它编码进 TCP 帧，本讲写进 WAL，
// 第 114 讲写进 Raft 日志，数值必须长期稳定。
type Op byte

const (
	OpGet Op = 1
	OpSet Op = 2
	OpDel Op = 3
)

// 哨兵错误沿用第 111 讲；本讲新增三条与持久化相关的错误，
// 调用方仍用 errors.Is 精确判定，不比对错误文本。
var (
	ErrNotFound     = errors.New("kv: key not found")
	ErrClosed       = errors.New("kv: storage closed")
	ErrEmptyKey     = errors.New("kv: empty key")
	ErrBadOp        = errors.New("kv: unknown op")
	ErrCorrupt      = errors.New("kv: wal record corrupt")
	ErrNoSpace      = errors.New("kv: no space left on device")
	ErrPartialWrite = errors.New("kv: partial write to wal")
)

// Command 是贯穿四讲唯一的数据载体：网络帧、WAL 记录、Raft 日志都只传递它。
type Command struct {
	Op    Op
	Key   string
	Value []byte
}

// Result 用 Found 把「键不存在」与「值恰好为空」区分开。
type Result struct {
	Value []byte
	Found bool
}

// Storage 是四讲唯一的抽象，Apply 是「执行一次命令」的完整语义。
type Storage interface {
	Apply(cmd Command) (Result, error)
	Close() error
}
