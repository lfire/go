// parse_fuzz_test.go —— 单文件即可 go test / go test -fuzz 运行
package parse

import (
	"strings"
	"testing"
)

// Count 统计分隔符个数，模糊测试用来找边界输入
func Count(s string) int {
	return strings.Count(s, ",")
}

func FuzzCount(f *testing.F) {
	seeds := []string{"", "a,b,c", ",,,", "abc"}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := Count(s)
		if got < 0 {
			t.Fatalf("计数不应为负: %d", got)
		}
		// 不变量：逗号个数不可能超过字符串长度
		if got > len(s) {
			t.Fatalf("计数 %d 超过长度 %d", got, len(s))
		}
	})
}
