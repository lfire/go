package kv

import "errors"

// 结构化错误码：让哨兵错误跨网络后仍能用 errors.Is 判定。
const (
	ErrCodeNone     byte = 0
	ErrCodeNotFound byte = 1
	ErrCodeClosed   byte = 2
	ErrCodeEmptyKey byte = 3
	ErrCodeBadOp    byte = 4
	ErrCodeBadFrame byte = 5
	ErrCodeInternal byte = 255
)

// ErrRemote 是无法映射到具体哨兵的远端错误，保留兜底但不泄露内部文案。
var ErrRemote = errors.New("kv: remote error")

var errByCode = map[byte]error{
	ErrCodeNotFound: ErrNotFound,
	ErrCodeClosed:   ErrClosed,
	ErrCodeEmptyKey: ErrEmptyKey,
	ErrCodeBadOp:    ErrBadOp,
	ErrCodeBadFrame: ErrBadFrame,
}

func encodeErr(err error) byte {
	switch {
	case err == nil:
		return ErrCodeNone
	case errors.Is(err, ErrNotFound):
		return ErrCodeNotFound
	case errors.Is(err, ErrClosed):
		return ErrCodeClosed
	case errors.Is(err, ErrEmptyKey):
		return ErrCodeEmptyKey
	case errors.Is(err, ErrBadOp):
		return ErrCodeBadOp
	case errors.Is(err, ErrBadFrame):
		return ErrCodeBadFrame
	default:
		return ErrCodeInternal
	}
}

func decodeErr(code byte) error {
	if code == ErrCodeNone {
		return nil
	}
	if e, ok := errByCode[code]; ok {
		return e
	}
	return ErrRemote
}

// isBusinessErr 区分业务错误与传输错误：前者连接仍健康可复用，
// 后者说明这条连接已不可信，连接池必须丢弃并补连。
func isBusinessErr(err error) bool {
	return errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrEmptyKey) ||
		errors.Is(err, ErrBadOp) ||
		errors.Is(err, ErrBadFrame)
}
