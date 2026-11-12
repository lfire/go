package main

import (
	"fmt"
	"log"
	"net/http"
)

// greeting 实现 ServeHTTP，因此它就是一个 http.Handler。
type greeting struct {
	prefix string
}

func (g greeting) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// PathValue 是 Go 1.22 新增的方法，读取路由模式里的通配段。
	name := r.PathValue("name")
	fmt.Fprintf(w, "%s, %s! method=%s\n", g.prefix, name, r.Method)
}

func health(w http.ResponseWriter, r *http.Request) {
	// 顺序很重要：先设响应头，再写状态码，最后写响应体。
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

func main() {
	mux := http.NewServeMux()

	// Go 1.22 起模式串支持 "METHOD /path/{wildcard}"，方法不匹配自动返回 405。
	mux.Handle("GET /hello/{name}", greeting{prefix: "你好"})
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	// HandlerFunc 把普通函数适配成 Handler；编译期就能检查接口是否满足。
	var _ http.Handler = http.HandlerFunc(health)

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
