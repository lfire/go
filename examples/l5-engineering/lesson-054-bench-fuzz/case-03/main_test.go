// join_bench_test.go —— 两个实现并排跑，供 benchstat 对比
package join

import (
	"strings"
	"testing"
)

// JoinPlus 用 += 拼接，每次循环都产生新字符串。
func JoinPlus(parts []string) string {
	s := ""
	for _, p := range parts {
		s += p + "-"
	}
	return s
}

// JoinBuilder 用 strings.Builder，复用底层缓冲。
func JoinBuilder(parts []string) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p)
		b.WriteByte('-')
	}
	return b.String()
}

func BenchmarkJoinPlus(b *testing.B) {
	parts := []string{"go", "test", "bench"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = JoinPlus(parts)
	}
}

func BenchmarkJoinBuilder(b *testing.B) {
	parts := []string{"go", "test", "bench"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = JoinBuilder(parts)
	}
}
