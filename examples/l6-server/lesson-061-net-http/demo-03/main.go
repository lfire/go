package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

// Middleware 是 Go HTTP 生态最通用的签名：吃一个 Handler，返回一个新 Handler。
type Middleware func(http.Handler) http.Handler

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("进入 %s %s", r.Method, r.URL.Path) // 前置：请求向内
		next.ServeHTTP(w, r)                            // 交棒给内层
		log.Printf("退出 %s 耗时 %v", r.URL.Path, time.Since(start)) // 后置：响应向外
	})
}

func recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic: %v", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// chain 按参数顺序包成洋葱：第一个参数位于最外层。
// 必须逆序循环，否则后写的中间件反而先被执行。
func chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "business logic")
	})
	// recoverPanic 放最外层，才能兜住业务和日志中间件里的 panic。
	handler := chain(mux, recoverPanic, logging)
	log.Fatal(http.ListenAndServe(":8080", handler))
}
