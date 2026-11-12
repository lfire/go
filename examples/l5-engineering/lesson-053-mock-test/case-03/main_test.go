// client_test.go —— 外部 HTTP 依赖用 httptest.NewServer 顶替
package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// FetchName 是被测函数（与测试同文件，保证单文件可 go test）
func FetchName(ctx context.Context, c *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func TestFetchName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ada"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := FetchName(ctx, srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("不应出错: %v", err)
	}
	if got != "ada" {
		t.Errorf("got %q, want ada", got)
	}
}
