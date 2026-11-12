// 文件：framecode.go
package kv

import (
	"errors"
	"fmt"
)

// 第 112 讲把错误文本塞进 status=2 的响应帧，导致 ErrNotFound 跨网络退化成
// 普通 error、errors.Is 失效。本讲把错误编码成稳定数值写进帧头的 status 字节，
// 客户端按同一张表还原成哨兵错误；数值与 Op 一样进入协议格式，长期不改。
// 16~22 为本讲占用，23 及以上预留给第 114 讲的分布式错误。
const (
	StatusOK     byte = 1
	CodeNotFound byte = 16
	CodeEmptyKey byte = 17
	CodeBadOp    byte = 18
	CodeClosed   byte = 19
	CodeCorrupt  byte = 20
	CodeNoSpace  byte = 21
	CodePartial  byte = 22
)

// codeOf 把服务端错误映射成帧状态码，未知错误统一收敛到 255。
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
	default:
		return 255
	}
}

// errOf 在客户端把状态码还原成哨兵错误，errors.Is 再次成立。
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
	default:
		return fmt.Errorf("kv: server status %d", code)
	}
}
