// 文件：contract.go
package kv

import "errors"

// 契约沿用第 111 讲定型、第 112 讲编码进帧、第 113 讲写进 WAL 的同一套定义。
// Op 数值、Command 字段、Storage 方法四讲之内一律不改，只追加分布式哨兵错误。
type Op byte

const (
	OpGet Op = 1
	OpSet Op = 2
	OpDel Op = 3
)

var (
	// 第 111 讲：错误模型
	ErrNotFound = errors.New("kv: key not found")
	ErrClosed   = errors.New("kv: storage closed")
	ErrEmptyKey = errors.New("kv: empty key")
	ErrBadOp    = errors.New("kv: unknown op")
	// 第 113 讲：持久化错误，跨网络仍可判定
	ErrCorrupt      = errors.New("kv: wal record corrupt")
	ErrNoSpace      = errors.New("kv: no space left on device")
	ErrPartialWrite = errors.New("kv: partial write to wal")
	// 第 114 讲：分布式错误
	ErrNotLeader     = errors.New("kv: not leader")
	ErrNoQuorum      = errors.New("kv: write lost quorum")
	ErrStaleRead     = errors.New("kv: stale read rejected")
	ErrSplitBrain    = errors.New("kv: split brain suspected")
	ErrMigrating     = errors.New("kv: shard is migrating")
	ErrCrossShardTxn = errors.New("kv: cross-shard transaction unsupported")
)

type Command struct {
	Op    Op
	Key   string
	Value []byte
}

type Result struct {
	Value []byte
	Found bool
}

type Storage interface {
	Apply(cmd Command) (Result, error)
	Close() error
}
