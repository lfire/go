// 文件：handler.go —— 网关串联：限流、预扣、入队、回补与结果轮询
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// Handler 的四个依赖以函数注入，单测里可任意替换，演示聚焦编排与回补。
type Handler struct {
	take    func() bool
	deduct  func(string) (bool, error)
	refund  func(string) error
	enqueue func(string) bool
	mu      sync.Mutex
	result  map[string]string
}

func NewHandler(take func() bool, deduct func(string) (bool, error),
	refund func(string) error, enqueue func(string) bool) *Handler {
	return &Handler{take: take, deduct: deduct, refund: refund,
		enqueue: enqueue, result: make(map[string]string)}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/seckill/result" {
		h.serveResult(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil || req.UserID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !h.take() {
		http.Error(w, "sold out", http.StatusTooManyRequests)
		return
	}
	ok, err := h.deduct(req.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if !ok {
		h.reply(w, "already processed", http.StatusOK)
		return
	}
	if !h.enqueue(req.UserID) {
		_ = h.refund(req.UserID) // 队列满必须回补，否则抢到却没订单
		http.Error(w, "system busy", http.StatusServiceUnavailable)
		return
	}
	h.mu.Lock()
	h.result[req.UserID] = "QUEUED"
	h.mu.Unlock()
	h.reply(w, "queued", http.StatusAccepted)
}

func (h *Handler) serveResult(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	h.mu.Lock()
	status := h.result[userID]
	h.mu.Unlock()
	if status == "" {
		status = "NOT_FOUND"
	}
	h.reply(w, status, http.StatusOK)
}

func (h *Handler) reply(w http.ResponseWriter, status string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
}

func main() {
	quotaLeft, stockLeft := 300, 100
	seen, enqueued := make(map[string]bool), 0
	var mu sync.Mutex
	h := NewHandler(
		func() bool {
			mu.Lock()
			defer mu.Unlock()
			if quotaLeft <= 0 {
				return false
			}
			quotaLeft--
			return true
		},
		func(u string) (bool, error) {
			mu.Lock()
			defer mu.Unlock()
			if seen[u] {
				return false, nil
			}
			if stockLeft <= 0 {
				return false, fmt.Errorf("sold out")
			}
			stockLeft--
			seen[u] = true
			return true, nil
		},
		func(u string) error {
			mu.Lock()
			defer mu.Unlock()
			if seen[u] {
				delete(seen, u)
				stockLeft++
			}
			return nil
		},
		func(u string) bool {
			mu.Lock()
			defer mu.Unlock()
			if enqueued >= 200 {
				return false
			}
			enqueued++
			return true
		},
	)
	accepted := 0
	for i := 0; i < 5000; i++ {
		req := httptest.NewRequest(http.MethodPost, "/seckill",
			strings.NewReader(fmt.Sprintf(`{"user_id":"u-%d"}`, i)))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusAccepted {
			accepted++
		}
	}
	fmt.Printf("accepted=%d enqueued=%d stockLeft=%d\n", accepted, enqueued, stockLeft)
}
