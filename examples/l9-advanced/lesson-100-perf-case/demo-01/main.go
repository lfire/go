package main

import (
	"fmt"
	"strconv"
	"testing"
)

type Item struct {
	Key   string
	Value int
}

// 基线：每条记录都走 fmt.Sprintf 反射，再用 += 累加，产生大量中间字符串。
func renderBaseline(items []Item) string {
	out := ""
	for _, it := range items {
		out += fmt.Sprintf("%s=%d;", it.Key, it.Value)
	}
	return out
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
	fmt.Println("结果长度 =", len(renderBaseline(items)))
	r := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = renderBaseline(items)
		}
	})
	fmt.Printf("基线 ns/op = %d，B/op = %d，allocs/op = %d\n",
		r.NsPerOp(), r.AllocedBytesPerOp(), r.AllocsPerOp())
}
