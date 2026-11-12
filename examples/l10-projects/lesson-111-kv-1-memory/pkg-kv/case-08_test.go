package kv

import (
	"strconv"
	"testing"
)

func BenchmarkMemorySet(b *testing.B) {
	s := NewMemory()
	defer s.Close()
	b.ReportAllocs()
	v := []byte("value")
	for i := 0; i < b.N; i++ {
		key := "k" + strconv.Itoa(i%1024)
		if _, err := s.Apply(Command{Op: OpSet, Key: key, Value: v}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMemoryGet(b *testing.B) {
	s := NewMemory()
	defer s.Close()
	_, _ = s.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Apply(Command{Op: OpGet, Key: "k"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkShardedSet(b *testing.B) {
	s := NewShardedMemory(32)
	defer s.Close()
	v := []byte("value")
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := "k" + strconv.Itoa(i%4096)
			if _, err := s.Apply(Command{Op: OpSet, Key: key, Value: v}); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}
