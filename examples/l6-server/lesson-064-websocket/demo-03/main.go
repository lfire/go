package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func sseHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	// 心跳注释行：SSE 里以冒号开头的行是注释，用来防止代理掐空闲连接。
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for i := 0; ; i++ {
		select {
		case <-r.Context().Done(): // 客户端关闭 EventSource 时立刻退出
			return
		case <-ticker.C:
			fmt.Fprintf(w, "id: %d\nevent: tick\ndata: %s\n\n",
				i, time.Now().Format(time.RFC3339))
			flusher.Flush() // 不 Flush 数据会卡在缓冲区里
		case <-keepalive.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /events", sseHandler)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "open /events with EventSource")
	})
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
