package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

// ready 控制就绪探针，退出前先置 false 主动摘流
var ready atomic.Bool

func main() {
	ready.Store(true)
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		// 存活探针只检查进程本身，绝不依赖数据库等下游
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// 就绪探针失败时返回 503，Service 会把本 Pod 摘掉
		if !ready.Load() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ready")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "hello from k8s")
	})

	srv := &http.Server{Addr: ":" + port, Handler: mux}
	go func() {
		log.Println("listening on", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	// K8s 删除 Pod 前发 SIGTERM，并给 terminationGracePeriodSeconds 窗口
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// 关键顺序：先摘流，留出时间让 Endpoint 更新，最后才停服
	ready.Store(false)
	log.Println("收到 SIGTERM，先置未就绪，等待流量摘除")
	time.Sleep(3 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Println("优雅退出超时:", err)
	}
	log.Println("已优雅退出")
}
