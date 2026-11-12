package main

import (
	"strings"
	"testing"
)

// allowedNext 是状态机合法转换的内存替身。
func allowedNext(from, to string) bool {
	rules := map[string]map[string]bool{
		"pending":   {"scheduled": true},
		"scheduled": {"running": true, "failed": true},
		"running":   {"succeeded": true, "failed": true, "timedout": true},
		"failed":    {"scheduled": true},
		"timedout":  {"scheduled": true},
		"succeeded": {},
	}
	return rules[from][to]
}

func TestStateMachine(t *testing.T) {
	cases := []struct {
		name     string
		from, to string
		want     bool
	}{
		{"pending 到 scheduled 合法", "pending", "scheduled", true},
		{"running 到 succeeded 合法", "running", "succeeded", true},
		{"failed 可重试", "failed", "scheduled", true},
		{"pending 到 running 非法", "pending", "running", false},
		{"终态不可再转", "succeeded", "running", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := allowedNext(tc.from, tc.to); got != tc.want {
				t.Fatalf("allowedNext(%q,%q) = %v, want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

func validCron(expr string) bool {
	return len(strings.Fields(expr)) == 5
}

func TestCronFields(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{"*/15 9-18 * * 1-5", true},
		{"0 0 * * *", true},
		{"* * * *", false},
		{"0 0 * * * *", false},
	}
	for _, tc := range cases {
		if got := validCron(tc.expr); got != tc.want {
			t.Errorf("validCron(%q) = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

func BenchmarkStateCheck(b *testing.B) {
	for i := 0; i < b.N; i++ {
		allowedNext("running", "succeeded")
	}
}
