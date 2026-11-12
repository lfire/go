package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
			fmt.Fprintln(w, "done")
		case <-r.Context().Done():
			// 客户端断开或超时就立刻退出，别让 goroutine 白等。
			log.Println("request cancelled")
		}
	})

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,  // 防慢请求头
		ReadTimeout:       5 * time.Second,  // 含 body 的读取上限
		WriteTimeout:      10 * time.Second, // 响应写回上限
		IdleTimeout:       60 * time.Second, // keep-alive 空闲回收
	}

	// 把 SIGINT / SIGTERM 转成 context 取消，这是服务端的标准写法。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Println("listening on :8080")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done() // 阻塞直到收到退出信号
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
	log.Println("server stopped")
}
