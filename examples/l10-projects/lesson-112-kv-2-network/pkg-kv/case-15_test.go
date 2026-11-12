package kv

import "testing"

func BenchmarkClientSet(b *testing.B) {
	s, err := NewServer("127.0.0.1:0", NewMemory())
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	go func() { _ = s.Serve() }()
	c, err := Dial(s.Addr())
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	v := []byte("value")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Apply(Command{Op: OpSet, Key: "k", Value: v}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPoolSet(b *testing.B) {
	s, err := NewServer("127.0.0.1:0", NewMemory())
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	go func() { _ = s.Serve() }()
	p, err := NewPool(s.Addr(), 8)
	if err != nil {
		b.Fatal(err)
	}
	defer p.Close()
	v := []byte("value")
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := p.Apply(Command{Op: OpSet, Key: "k", Value: v}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
