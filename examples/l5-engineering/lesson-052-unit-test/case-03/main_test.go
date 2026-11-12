// split_test.go —— 单个文件即可 go test 通过
package split

import (
	"strings"
	"testing"
)

// Split 是被测函数（与测试同文件，保证单文件可 go test）
func Split(s, sep string) []string {
	return strings.Split(s, sep)
}

func TestSplit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		sep  string
		want int
	}{
		{"逗号分隔", "a,b,c", ",", 3},
		{"无分隔符", "abc", ",", 1},
		{"空串", "", ",", 1},
	}
	for _, c := range cases {
		c := c // Go 1.22 起可省；Go 1.21 必须复制
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := Split(c.in, c.sep)
			if len(got) != c.want {
				t.Fatalf("段数 got %d, want %d", len(got), c.want)
			}
		})
	}
}
