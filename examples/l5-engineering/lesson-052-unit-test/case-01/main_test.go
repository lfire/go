// price_test.go —— 单独一个文件即可 go test 通过
package price

import "testing"

// 被测函数：按数量计算折后总价（示例起见与测试同文件）
func Total(unit, count int, member bool) int {
	sum := unit * count
	if member {
		sum = sum * 9 / 10
	}
	return sum
}

func TestTotal(t *testing.T) {
	cases := []struct {
		name   string
		unit   int
		count  int
		member bool
		want   int
	}{
		{"普通用户无折扣", 100, 2, false, 200},
		{"会员九折", 100, 2, true, 180},
		{"零数量", 100, 0, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Total(c.unit, c.count, c.member)
			assertEqual(t, got, c.want)
		})
	}
}

// t.Helper 让失败信息指向调用它的那一行，而不是辅助函数内部
func assertEqual(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}
