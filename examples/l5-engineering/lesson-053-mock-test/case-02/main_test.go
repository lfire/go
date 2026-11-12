// handler_test.go —— 用标准库 httptest 测 Handler，无需真实端口
package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Ping 是被测处理器（示例起见与测试同文件）
func Ping(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func TestPing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	Ping(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 got %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type got %q", ct)
	}
	if got := rec.Body.String(); got != `{"ok":true}` {
		t.Errorf("响应体 got %q", got)
	}
}
