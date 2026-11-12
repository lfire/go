package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// cancelAware 模拟一个会透传 ctx 的下游调用；取消后应立即返回。
func cancelAware(ctx context.Context, started chan struct{}) error {
	close(started)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Second):
		return nil
	}
}

func TestRequestContextCancelledOnClientGone(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = cancelAware(r.Context(), started)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	done := make(chan struct{})
	go func() {
		_, _ = http.DefaultClient.Do(req)
		close(done)
	}()
	<-started
	cancel() // 模拟客户端断开
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("客户端已取消，但服务端未及时返回")
	}
}
