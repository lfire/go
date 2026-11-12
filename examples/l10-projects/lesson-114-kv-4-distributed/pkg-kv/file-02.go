// 文件：framecode.go
package kv

import (
	"errors"
	"fmt"
)

// 帧 status 位沿用第 113 讲预留的高位段：16~22 是单机持久化错误，
// 23 起是分布式错误。数值与 Op 一样进入协议格式，客户端 errOf 与服务端
// codeOf 共用同一张表，因此第 112 讲退化成文本的错误重新可判定。
const (
	StatusOK       byte = 1
	CodeNotFound   byte = 16
	CodeEmptyKey   byte = 17
	CodeBadOp      byte = 18
	CodeClosed     byte = 19
	CodeCorrupt    byte = 20
	CodeNoSpace    byte = 21
	CodePartial    byte = 22
	CodeNotLeader  byte = 23
	CodeNoQuorum   byte = 24
	CodeStaleRead  byte = 25
	CodeMigrating  byte = 26
	CodeCrossShard byte = 27
	CodeSplitBrain byte = 28
)

func codeOf(err error) byte {
	switch {
	case err == nil:
		return StatusOK
	case errors.Is(err, ErrNotFound):
		return CodeNotFound
	case errors.Is(err, ErrEmptyKey):
		return CodeEmptyKey
	case errors.Is(err, ErrBadOp):
		return CodeBadOp
	case errors.Is(err, ErrClosed):
		return CodeClosed
	case errors.Is(err, ErrCorrupt):
		return CodeCorrupt
	case errors.Is(err, ErrNoSpace):
		return CodeNoSpace
	case errors.Is(err, ErrPartialWrite):
		return CodePartial
	case errors.Is(err, ErrNotLeader):
		return CodeNotLeader
	case errors.Is(err, ErrNoQuorum):
		return CodeNoQuorum
	case errors.Is(err, ErrStaleRead):
		return CodeStaleRead
	case errors.Is(err, ErrSplitBrain):
		return CodeSplitBrain
	case errors.Is(err, ErrMigrating):
		return CodeMigrating
	case errors.Is(err, ErrCrossShardTxn):
		return CodeCrossShard
	default:
		return 255
	}
}

func errOf(code byte) error {
	switch code {
	case StatusOK:
		return nil
	case CodeNotFound:
		return ErrNotFound
	case CodeEmptyKey:
		return ErrEmptyKey
	case CodeBadOp:
		return ErrBadOp
	case CodeClosed:
		return ErrClosed
	case CodeCorrupt:
		return ErrCorrupt
	case CodeNoSpace:
		return ErrNoSpace
	case CodePartial:
		return ErrPartialWrite
	case CodeNotLeader:
		return ErrNotLeader
	case CodeNoQuorum:
		return ErrNoQuorum
	case CodeStaleRead:
		return ErrStaleRead
	case CodeMigrating:
		return ErrMigrating
	case CodeCrossShard:
		return ErrCrossShardTxn
	case CodeSplitBrain:
		return ErrSplitBrain
	default:
		return fmt.Errorf("kv: server status %d", code)
	}
}
