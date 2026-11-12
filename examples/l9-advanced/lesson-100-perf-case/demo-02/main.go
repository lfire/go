package main

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

type Item struct {
	Key   string
	Value int
}

// 优化版：预分配 strings.Builder，避免 fmt 反射与中间字符串。
func renderFast(items []Item) string {
	var b strings.Builder
	b.Grow(len(items) * 12)
	for i := range items {
		b.WriteString(items[i].Key)
		b.WriteByte('=')
		b.WriteString(strconv.Itoa(items[i].Value))
		b.WriteByte(';')
	}
	return b.String()
}

func makeItems(n int) []Item {
	items := make([]Item, n)
	for i := range items {
		items[i] = Item{Key: "k" + strconv.Itoa(i%16), Value: i}
	}
	return items
}

func main() {
	items := makeItems(32)
	fmt.Println("结果长度 =", len(renderFast(items)))
	r := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = renderFast(items)
		}
	})
	fmt.Printf("优化 ns/op = %d，B/op = %d，allocs/op = %d\n",
		r.NsPerOp(), r.AllocedBytesPerOp(), r.AllocsPerOp())
}
