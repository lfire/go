package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
)

// Sampler 按 TraceID 做确定性采样，保证同一条 Trace 各处结论一致。
type Sampler struct{ ratio float64 }

func (s Sampler) ShouldSample(traceID string) bool {
	var h uint32
	for _, b := range []byte(traceID) {
		h = h*31 + uint32(b)
	}
	return float64(h%100)/100 < s.ratio
}

func traceIDFrom(h http.Header) string {
	parts := strings.Split(h.Get("traceparent"), "-")
	if len(parts) >= 2 && len(parts[1]) == 32 {
		return parts[1]
	}
	return "4bf92f3577b34da6a3ce929d0e0e4736" // 无头时新生成，这里用固定值演示
}

// TracingMiddleware 提取上下文，并把 trace_id 注入日志，实现日志与链路关联。
func TracingMiddleware(sample Sampler, next http.Handler) http.Handler {
	logger := log.New(os.Stdout, "", 0)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid := traceIDFrom(r.Header)
		if sample.ShouldSample(tid) {
			logger.Printf("trace_id=%s path=%s event=start", tid, r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	ts := httptest.NewServer(TracingMiddleware(Sampler{ratio: 1},
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintln(w, "ok")
		})))
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/order", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	resp, _ := http.DefaultClient.Do(req)
	fmt.Println("响应状态:", resp.StatusCode)
	resp.Body.Close()
}
