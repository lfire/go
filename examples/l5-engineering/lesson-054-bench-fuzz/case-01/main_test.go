// concat_bench_test.go —— 单文件即可 go test -bench 运行
package concat

import (
	"strings"
	"testing"
)

// Join 是被测函数（与测试同文件，保证单文件可跑）
func Join(parts []string) string {
	return strings.Join(parts, "-")
}

func BenchmarkJoin(b *testing.B) {
	parts := []string{"go", "test", "bench"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Join(parts)
	}
}
