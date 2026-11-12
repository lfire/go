package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

// exposition 手写 Prometheus 文本格式：类型声明加样本行。
func exposition(reqTotal, errTotal uint64, p95 float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# HELP http_requests_total 累计请求数\n")
	fmt.Fprintf(&b, "# TYPE http_requests_total counter\n")
	fmt.Fprintf(&b, "http_requests_total{service=\"order\"} %d\n", reqTotal)
	fmt.Fprintf(&b, "# TYPE http_errors_total counter\n")
	fmt.Fprintf(&b, "http_errors_total{service=\"order\"} %d\n", errTotal)
	fmt.Fprintf(&b, "# TYPE http_request_duration_seconds gauge\n")
	fmt.Fprintf(&b, "http_request_duration_seconds{quantile=\"0.95\"} %g\n", p95)
	return b.String()
}

func main() {
	// 1. 直接看 exposition 文本
	fmt.Println("--- exposition 文本 ---")
	fmt.Print(exposition(1000, 12, 0.18))

	// 2. 通过 /metrics 端点暴露给 Prometheus 抓取
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprint(w, exposition(1000, 12, 0.18))
	}))
	defer ts.Close()
	resp, _ := http.Get(ts.URL + "/metrics")
	defer resp.Body.Close()
	fmt.Println("抓取状态:", resp.StatusCode, "类型:", resp.Header.Get("Content-Type"))
}
