package etl

import "testing"

// windowOf 返回事件所属的窗口起点；负数时间戳取模需显式归一化。
func windowOf(ts, size int64) int64 {
	r := ts % size
	if r < 0 {
		r += size
	}
	return ts - r
}

func TestWindowOf_TableDriven(t *testing.T) {
	cases := []struct {
		name string
		ts   int64
		want int64
	}{
		{"窗口内偏移 10", 10, 0},
		{"窗口末尾 59", 59, 0},
		{"恰好下一窗口 60", 60, 60},
		{"下一窗口内 61", 61, 60},
		{"跨两窗 125", 125, 120},
		{"负数时间 -1", -1, -60},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := windowOf(c.ts, 60); got != c.want {
				t.Fatalf("windowOf(%d)=%d, want %d", c.ts, got, c.want)
			}
		})
	}
}

func BenchmarkWindowOf(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = windowOf(int64(i), 60)
	}
}
