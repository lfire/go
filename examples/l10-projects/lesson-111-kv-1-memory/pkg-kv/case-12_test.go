package kv

import (
	"errors"
	"testing"
	"time"
)

// storageFactories 让同一套契约测试跑在每一个实现上；后续讲次的新实现
// 只要加进这个列表，就能复用全部断言，证明「换实现、不换契约」。
func storageFactories() map[string]func() Storage {
	return map[string]func() Storage{
		"memory":  func() Storage { return NewMemory() },
		"sharded": func() Storage { return NewShardedMemory(8) },
		"legacy":  func() Storage { return WrapLegacy(&tinyLegacy{m: make(map[string]string)}, ErrNotFound) },
		"metered": func() Storage { return NewMeteredStorage(NewMemory()) },
		"ttl":     func() Storage { return NewTTLMemory(NewMemory(), time.Hour) },
	}
}

func TestStorageContract(t *testing.T) {
	for name, factory := range storageFactories() {
		t.Run(name, func(t *testing.T) {
			s := factory()
			defer s.Close()
			if _, err := s.Apply(Command{Op: OpGet, Key: "missing"}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("want ErrNotFound, got %v", err)
			}
			if _, err := s.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
				t.Fatal(err)
			}
			r, err := s.Apply(Command{Op: OpGet, Key: "k"})
			if err != nil || !r.Found || string(r.Value) != "v" {
				t.Fatalf("got %+v err=%v", r, err)
			}
			if _, err := s.Apply(Command{Op: OpDel, Key: "k"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Apply(Command{Op: OpGet, Key: "k"}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("want ErrNotFound after del, got %v", err)
			}
			if _, err := s.Apply(Command{Op: 99, Key: "k"}); !errors.Is(err, ErrBadOp) {
				t.Fatalf("want ErrBadOp, got %v", err)
			}
			if _, err := s.Apply(Command{Op: OpSet, Key: "", Value: []byte("x")}); !errors.Is(err, ErrEmptyKey) {
				t.Fatalf("want ErrEmptyKey, got %v", err)
			}
		})
	}
}
