package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func Encode62(n uint64) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 11)
	for n > 0 {
		b = append(b, alphabet[n%62])
		n /= 62
	}
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

func Decode62(s string) (uint64, bool) {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	if s == "" {
		return 0, false
	}
	var n uint64
	for i := 0; i < len(s); i++ {
		idx := strings.IndexByte(alphabet, s[i])
		if idx < 0 {
			return 0, false
		}
		n = n*62 + uint64(idx)
	}
	return n, true
}

func TestBase62RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		id   uint64
	}{
		{"零值", 0},
		{"个位", 1},
		{"进位边界", 61},
		{"两位起点", 62},
		{"大数", 1 << 40},
		{"uint64 上限", ^uint64(0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code := Encode62(c.id)
			back, ok := Decode62(code)
			if !ok || back != c.id {
				t.Fatalf("往返失败 id=%d code=%s back=%d ok=%v", c.id, code, back, ok)
			}
		})
	}
}

func TestDecode62RejectsBadChar(t *testing.T) {
	if _, ok := Decode62("a!b"); ok {
		t.Fatal("非法字符应被拒绝")
	}
	if _, ok := Decode62(""); ok {
		t.Fatal("空串应被拒绝")
	}
}

func TestRedirectUsesFoundNotMovedPermanently(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://go.dev", http.StatusFound)
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(srv.URL + "/abc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("期望 302, 得到 %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "https://go.dev" {
		t.Fatalf("Location 不符: %s", got)
	}
}

func BenchmarkEncode62(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Encode62(uint64(i))
	}
}
