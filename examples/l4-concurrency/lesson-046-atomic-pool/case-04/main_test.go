package main

import (
	"sync"
	"testing"
)

type Buf struct{ Data []byte }

var bufPool = sync.Pool{
	New: func() any { return &Buf{Data: make([]byte, 0, 1024)} },
}

var sink []byte

func BenchmarkNoPool(b *testing.B) {
	for i := 0; i < b.N; i++ {
		sink = make([]byte, 0, 1024)
	}
}

func BenchmarkWithPool(b *testing.B) {
	for i := 0; i < b.N; i++ {
		bb := bufPool.Get().(*Buf)
		bb.Data = bb.Data[:0]
		bufPool.Put(bb)
	}
}

// 错误写法：取出后不重置直接 append，残留数据混进结果；归还时也没清
func fillNoReset(b *Buf, s string) []byte {
	b.Data = append(b.Data, s...)
	return append([]byte(nil), b.Data...)
}

// 正确写法：取出先截零，用完后归还前再截零
func fillReset(b *Buf, s string) []byte {
	b.Data = append(b.Data[:0], s...)
	out := append([]byte(nil), b.Data...)
	b.Data = b.Data[:0] // 归还前重置
	return out
}

// 从池取用并归还的包装：真正的 bug 是「忘了重置」
func withPool(fn func(*Buf, string) []byte, s string) []byte {
	b := bufPool.Get().(*Buf)
	out := fn(b, s)
	bufPool.Put(b)
	return out
}

func TestPoolReset(t *testing.T) {
	// -race 下 sync/pool.go 的 Put 会随机丢弃对象，复用顺序不可依赖，
	// 因此直接构造「带残留」的脏对象，让实验稳定复现
	dirty := &Buf{Data: []byte("旧数据")}
	if got := string(fillNoReset(dirty, "新")); got != "旧数据新" {
		t.Fatalf("未重置应产生污染，实际 %q", got)
	}
	clean := &Buf{Data: []byte("旧数据")}
	if got := string(fillReset(clean, "新")); got != "新" {
		t.Fatalf("重置后应干净，实际 %q", got)
	}
	// 正确写法放进真实 Pool 也稳定
	if got := string(withPool(fillReset, "新")); got != "新" {
		t.Fatalf("Pool 中重置写法应稳定，实际 %q", got)
	}
}
