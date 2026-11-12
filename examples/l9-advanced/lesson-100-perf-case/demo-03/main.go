package main

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

type Item struct {
	Key   string
	Value int
}

func makeItems(n int) []Item {
	items := make([]Item, n)
	for i := range items {
		items[i] = Item{Key: "k" + strconv.Itoa(i%16), Value: i}
	}
	return items
}

func renderBaseline(items []Item) string {
	out := ""
	for _, it := range items {
		out += fmt.Sprintf("%s=%d;", it.Key, it.Value)
	}
	return out
}

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

func bench(f func()) testing.BenchmarkResult {
	return testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			f()
		}
	})
}

func main() {
	items := makeItems(32)
	base := bench(func() { _ = renderBaseline(items) })
	fast := bench(func() { _ = renderFast(items) })

	fmt.Printf("基线 %d ns/op，优化 %d ns/op\n", base.NsPerOp(), fast.NsPerOp())
	fmt.Printf("分配 %d 降到 %d B/op\n", base.AllocedBytesPerOp(), fast.AllocedBytesPerOp())
	if fast.NsPerOp() >= base.NsPerOp() {
		fmt.Println("验证失败：出现回归")
	} else {
		fmt.Println("验证通过")
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Println("累计 GC 次数 =", m.NumGC)
}
