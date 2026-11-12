// 文件：gateway.go —— HTTP/JSON 网关：幂等键透传、超时传递、错误映射
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

var ErrStockNotEnough = errors.New("stock not enough")

// Downstream 是网关对编排层的抽象（真实实现是 gRPC 客户端）。
type Downstream interface {
	CreateOrder(ctx context.Context, idemKey, userID, sku string, qty int) (string, error)
}

type stubDownstream struct{}

func (stubDownstream) CreateOrder(ctx context.Context, idemKey, userID, sku string, qty int) (string, error) {
	select {
	case <-time.After(20 * time.Millisecond):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if sku == "SOLD-OUT" {
		return "", ErrStockNotEnough
	}
	return "O-" + idemKey, nil
}

// Gateway 把请求级 context 一路透传到下游，超时不再是假超时。
type Gateway struct {
	down    Downstream
	timeout time.Duration
}

func NewGateway(d Downstream, timeout time.Duration) *Gateway {
	return &Gateway{down: d, timeout: timeout}
}

// statusOf 把内部错误映射成稳定状态码，避免泄露内部细节。
func statusOf(err error) int {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	case errors.Is(err, ErrStockNotEnough):
		return http.StatusConflict
	default:
		return http.StatusBadGateway
	}
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idemKey := r.Header.Get("Idempotency-Key")
	if idemKey == "" || strings.ContainsAny(idemKey, " \t\r\n") {
		http.Error(w, "Idempotency-Key required", http.StatusBadRequest)
		return
	}
	var body struct {
		UserID string `json:"user_id"`
		SKU    string `json:"sku"`
		Qty    int    `json:"qty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Qty <= 0 || body.Qty > 100 || body.SKU == "" {
		http.Error(w, "invalid qty or sku", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), g.timeout)
	defer cancel()
	orderID, err := g.down.CreateOrder(ctx, idemKey, body.UserID, body.SKU, body.Qty)
	if err != nil {
		code := statusOf(err)
		if code == http.StatusGatewayTimeout {
			w.Header().Set("Retry-After", "1")
		}
		http.Error(w, http.StatusText(code), code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"order_id": orderID, "status": "CREATED"})
}

func main() {
	gw := NewGateway(stubDownstream{}, 500*time.Millisecond)
	req := httptest.NewRequest(http.MethodPost, "/orders",
		strings.NewReader(`{"user_id":"u1","sku":"SKU-9","qty":2}`))
	req.Header.Set("Idempotency-Key", "idem-abc")
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)
	fmt.Printf("status=%d body=%s", rec.Code, rec.Body.String())
}
