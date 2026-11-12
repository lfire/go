package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type ctxKey string // 自定义非导出键类型

const traceKey ctxKey = "trace-id"

func handler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // 客户端断开自动取消
	ctx = context.WithValue(ctx, traceKey, "req-9527")

	// 给下游单独设更短的超时，慢依赖不拖住请求
	callCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if err := queryDB(callCtx); err != nil {
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
		return
	}
	fmt.Fprintln(w, "ok", traceID(ctx))
}

func traceID(ctx context.Context) string {
	if v, ok := ctx.Value(traceKey).(string); ok {
		return v // 取值必须判 ok
	}
	return "unknown"
}

func queryDB(ctx context.Context) error {
	select {
	case <-time.After(200 * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func main() {
	http.HandleFunc("/", handler)
	_ = http.ListenAndServe(":8080", nil)
}
