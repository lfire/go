// 文件：internal/gateway/sse.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

type Chunk struct {
	Text string
	Done bool
}

// StreamToClient 把统一块流以 SSE 写给下游：逐块 Flush、禁反代缓冲、感知断开。
func StreamToClient(w http.ResponseWriter, r *http.Request, stream func(emit func(Chunk) error) error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	started := false // 是否已发出首个数据块，决定失败后能否重试
	err := stream(func(c Chunk) error {
		select {
		case <-r.Context().Done():
			return r.Context().Err() // 客户端断开，立刻停止上游
		default:
		}
		payload, _ := json.Marshal(map[string]any{"text": c.Text, "done": c.Done})
		if _, e := fmt.Fprintf(w, "data: %s\n\n", payload); e != nil {
			return e
		}
		flusher.Flush()
		started = true
		return nil
	})
	if err != nil {
		// 响应头已发出不能改状态码；半流失败绝不重试，只能写 error 事件。
		msg := fmt.Sprintf("stream failed: %v; retryable=%t", err, !started)
		fmt.Fprintf(w, "event: error\ndata: %q\n\n", msg)
		flusher.Flush()
	}
}

// PipeWithBackpressure 用有界 channel 连接上游解析与下游写，满则上游阻塞。
func PipeWithBackpressure(ctx context.Context, w http.ResponseWriter, r *http.Request,
	bufSize int, upstream func(emit func(Chunk) error) error) error {
	ch := make(chan Chunk, bufSize)
	upErr := make(chan error, 1)
	go func() {
		defer close(ch)
		upErr <- upstream(func(c Chunk) error {
			select {
			case ch <- c: // 缓冲满则在此阻塞，形成背压
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	flusher := w.(http.Flusher)
	for c := range ch {
		select {
		case <-r.Context().Done():
			return r.Context().Err()
		default:
		}
		payload, _ := json.Marshal(map[string]any{"text": c.Text, "done": c.Done})
		if _, e := fmt.Fprintf(w, "data: %s\n\n", payload); e != nil {
			return e
		}
		flusher.Flush()
	}
	return <-upErr
}

func readAll(resp *http.Response) string {
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 256)
	for {
		n, e := resp.Body.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if e != nil {
			break
		}
	}
	return sb.String()
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/direct", func(w http.ResponseWriter, r *http.Request) {
		StreamToClient(w, r, func(emit func(Chunk) error) error {
			for _, s := range []string{"你", "好", "世", "界"} {
				if e := emit(Chunk{Text: s}); e != nil {
					return e
				}
				time.Sleep(time.Millisecond)
			}
			return emit(Chunk{Done: true})
		})
	})
	mux.HandleFunc("/bp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		_ = PipeWithBackpressure(r.Context(), w, r, 2, func(emit func(Chunk) error) error {
			for _, s := range []string{"a", "b", "c"} {
				if e := emit(Chunk{Text: s}); e != nil {
					return e
				}
			}
			return emit(Chunk{Done: true})
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/direct")
	if err != nil {
		panic(err)
	}
	direct := readAll(resp)
	fmt.Println("content-type:", resp.Header.Get("Content-Type"))
	fmt.Println("direct frames:", strings.Count(direct, "data:"))
	resp2, err := http.Get(srv.URL + "/bp")
	if err != nil {
		panic(err)
	}
	bp := readAll(resp2)
	fmt.Println("bp frames:", strings.Count(bp, "data:"))
}
