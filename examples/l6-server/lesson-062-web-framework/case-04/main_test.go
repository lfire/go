package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.PathValue("id")))
	})
	return mux
}

func TestMethodAndPathValue(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "42" {
		t.Fatalf("GET: code=%d body=%q", rec.Code, rec.Body.String())
	}
	rec2 := httptest.NewRecorder()
	newMux().ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/users/42", nil))
	if rec2.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: code=%d, want 405", rec2.Code)
	}
	if allow := rec2.Header().Get("Allow"); allow == "" {
		t.Fatal("405 未带 Allow 头")
	}
}

func BenchmarkRouteOnly(b *testing.B) {
	mux := newMux()
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mux.ServeHTTP(httptest.NewRecorder(), req)
	}
}

func BenchmarkRouteWithDB(b *testing.B) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Millisecond) // 模拟一次数据库往返
	})
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mux.ServeHTTP(httptest.NewRecorder(), req)
	}
}
