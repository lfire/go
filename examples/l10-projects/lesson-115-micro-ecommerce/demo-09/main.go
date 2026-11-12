// 文件：observability.go —— 网关埋点中间件与结构化日志字段
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"
)

// metricsSink 用两个函数抽象 Counter 与 Histogram，生产里换成 Prometheus。
type metricsSink struct {
	inc     func(path string, code int)
	observe func(ms int64)
}

// statusWriter 捕获真实状态码，否则 WriteHeader 之后拿不到 code。
type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

// withMetrics 记录每个请求的耗时与状态，并把 trace_id 注入日志上下文。
func withMetrics(next http.Handler, m metricsSink) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: 200}
		traceID := r.Header.Get("X-Trace-Id")
		next.ServeHTTP(sw, r)
		m.inc(r.URL.Path, sw.code)
		m.observe(time.Since(start).Milliseconds())
		_ = traceID // 真实实现写入 logger.With("trace_id", traceID)
	})
}

func main() {
	var paths []string
	m := metricsSink{
		inc:     func(path string, code int) { paths = append(paths, fmt.Sprintf("%s:%d", path, code)) },
		observe: func(ms int64) {},
	}
	h := withMetrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}), m)
	req := httptest.NewRequest(http.MethodPost, "/orders", nil)
	req.Header.Set("X-Trace-Id", "trace-abc")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	fmt.Println("recorded:", paths)
}
