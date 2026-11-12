package kv

import "errors"

// LegacyStore 是第 25 讲定义的「按操作拆方法、值为 string」的存储形状。
type LegacyStore interface {
	Put(key, value string) error
	Get(key string) (string, error)
	Delete(key string) error
	Keys() []string
}

var _ Storage = (*legacyAdapter)(nil)

// WrapLegacy 把第 25 讲风格的实现适配成本讲的 Storage。
// notFound 由调用方传入旧包的哨兵错误，适配器用 errors.Is 翻译，
// 让错误链在新旧两层都可判定，旧实现一行都不用改。
func WrapLegacy(l LegacyStore, notFound error) Storage {
	return &legacyAdapter{inner: l, notFound: notFound}
}

type legacyAdapter struct {
	inner    LegacyStore
	notFound error
}

func (a *legacyAdapter) Apply(cmd Command) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	switch cmd.Op {
	case OpSet:
		return Result{Found: true}, a.inner.Put(cmd.Key, string(cmd.Value))
	case OpGet:
		v, err := a.inner.Get(cmd.Key)
		if err != nil {
			if a.notFound != nil && errors.Is(err, a.notFound) {
				return Result{}, ErrNotFound
			}
			return Result{}, err
		}
		return Result{Value: []byte(v), Found: true}, nil
	case OpDel:
		err := a.inner.Delete(cmd.Key)
		if err != nil {
			if a.notFound != nil && errors.Is(err, a.notFound) {
				return Result{}, ErrNotFound
			}
			return Result{}, err
		}
		return Result{Found: true}, nil
	default:
		return Result{}, ErrBadOp
	}
}

func (a *legacyAdapter) Close() error { return nil }
