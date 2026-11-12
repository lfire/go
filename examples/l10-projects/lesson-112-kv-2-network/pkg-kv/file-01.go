package kv

import "errors"

// Op 用显式数值定义：第 112 讲把它编码进 TCP 帧，
// 第 113、114 讲写进 WAL 与 Raft 日志，数值必须长期稳定。
type Op byte

const (
	OpGet Op = 1
	OpSet Op = 2
	OpDel Op = 3
)

// 包级哨兵错误：调用方统一用 errors.Is 判定，不比对错误文本。
var (
	ErrNotFound = errors.New("kv: key not found")
	ErrClosed   = errors.New("kv: storage closed")
	ErrEmptyKey = errors.New("kv: empty key")
	ErrBadOp    = errors.New("kv: unknown op")
)

// Command 是四讲唯一的数据载体：解析、网络帧、WAL、Raft 都只传它。
type Command struct {
	Op    Op
	Key   string
	Value []byte
}

// Result 用 Found 把「键不存在」与「值恰好为空」分开。
type Result struct {
	Value []byte
	Found bool
}

// Storage 是四讲唯一的抽象：Apply 必须是「执行一次命令」的完整语义，
// 可重放、可持久化、可复制，任何一版实现都要满足它。
type Storage interface {
	Apply(cmd Command) (Result, error)
	Close() error
}
