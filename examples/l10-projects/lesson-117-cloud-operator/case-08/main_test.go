// 文件：internal/reconcile/reconcile_test.go
package main

import "testing"

// 本段自带被测纯函数，保证单独 go test 即可编译运行。
type spec struct {
	replicas int
	version  string
}

type observed struct {
	ready   int
	version string
}

func decide(w spec, g observed) string {
	if g.version != w.version {
		return "rollout"
	}
	if g.ready != w.replicas {
		return "scale"
	}
	return "none"
}

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		want spec
		got  observed
		act  string
	}{
		{"版本不同优先升级", spec{3, "v2.0.0"}, observed{3, "v1.4.0"}, "rollout"},
		{"副本不足扩容", spec{5, "v1.4.0"}, observed{3, "v1.4.0"}, "scale"},
		{"副本过多缩容", spec{2, "v1.4.0"}, observed{3, "v1.4.0"}, "scale"},
		{"已收敛无动作", spec{3, "v1.4.0"}, observed{3, "v1.4.0"}, "none"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := decide(c.want, c.got); got != c.act {
				t.Fatalf("decide=%s want=%s", got, c.act)
			}
		})
	}
}

func TestDecideIdempotent(t *testing.T) {
	w := spec{replicas: 3, version: "v2.0.0"}
	g := observed{ready: 3, version: "v2.0.0"}
	first := decide(w, g)
	for i := 0; i < 3; i++ {
		if got := decide(w, g); got != first {
			t.Fatalf("call %d changed result: %s", i, got)
		}
	}
}

func BenchmarkDecide(b *testing.B) {
	w := spec{replicas: 3, version: "v2.0.0"}
	g := observed{ready: 3, version: "v1.4.0"}
	for i := 0; i < b.N; i++ {
		_ = decide(w, g)
	}
}
