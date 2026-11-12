// 用 context 传递 trace_id，让同一条请求的日志自动带上同一字段
package main

import (
	"context"
	"log/slog"
	"os"
)

type ctxKey string

const traceKey ctxKey = "trace_id"

// WithTrace 派生绑定 trace_id 的 logger，并把 id 放进 context
func WithTrace(ctx context.Context, base *slog.Logger, traceID string) (*slog.Logger, context.Context) {
	ctx = context.WithValue(ctx, traceKey, traceID)
	return base.With("trace_id", traceID), ctx
}

func main() {
	base := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	log, ctx := WithTrace(context.Background(), base, "t-8f3c")
	log.Info("请求进入", "path", "/order")
	handle(ctx, log)
}

func handle(ctx context.Context, log *slog.Logger) {
	// 从 context 取回 trace_id，跨函数仍能关联同一条请求
	tid, _ := ctx.Value(traceKey).(string)
	log.Info("查询完成", "trace_from_ctx", tid, "rows", 1)
}
