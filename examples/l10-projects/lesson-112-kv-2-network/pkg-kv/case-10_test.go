package kv

import (
	"errors"
	"strconv"
	"sync"
	"testing"
)

func startTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	s, err := NewServer("127.0.0.1:0", NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = s.Serve() }()
	return s, s.Addr()
}

func TestClientServerSETGET(t *testing.T) {
	s, addr := startTestServer(t)
	defer s.Close()
	c, err := Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
		t.Fatal(err)
	}
	r, err := c.Apply(Command{Op: OpGet, Key: "k"})
	if err != nil || !r.Found || string(r.Value) != "v" {
		t.Fatalf("got %+v err=%v", r, err)
	}
	if _, err := c.Apply(Command{Op: OpGet, Key: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestPoolParallel(t *testing.T) {
	s, addr := startTestServer(t)
	defer s.Close()
	p, err := NewPool(addr, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "k" + strconv.Itoa(i)
			if _, err := p.Apply(Command{Op: OpSet, Key: key, Value: []byte("v")}); err != nil {
				t.Errorf("set %s: %v", key, err)
			}
		}(i)
	}
	wg.Wait()
}
