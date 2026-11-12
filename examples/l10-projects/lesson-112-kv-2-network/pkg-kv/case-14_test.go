package kv

import (
	"errors"
	"testing"
)

func TestRemoteStorageContract(t *testing.T) {
	s, addr := startTestServer(t)
	defer s.Close()
	c, err := Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p, err := NewPool(addr, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for name, st := range map[string]Storage{"client": c, "pool": p} {
		t.Run(name, func(t *testing.T) {
			if _, err := st.Apply(Command{Op: OpGet, Key: "absent"}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("want ErrNotFound, got %v", err)
			}
			if _, err := st.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
				t.Fatal(err)
			}
			r, err := st.Apply(Command{Op: OpGet, Key: "k"})
			if err != nil || !r.Found || string(r.Value) != "v" {
				t.Fatalf("got %+v err=%v", r, err)
			}
			if _, err := st.Apply(Command{Op: OpGet, Key: ""}); !errors.Is(err, ErrEmptyKey) {
				t.Fatalf("want ErrEmptyKey, got %v", err)
			}
		})
	}
}
