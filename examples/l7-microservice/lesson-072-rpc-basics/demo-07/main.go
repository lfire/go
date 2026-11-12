package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"
)

type request struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}
type response struct {
	ID     uint64 `json:"id"`
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

// Client 就是桩：把远程调用伪装成本地方法，拼报文、发请求、解响应都被藏起来。
type Client struct {
	endpoint string
	httpc    *http.Client
	seq      uint64
}

func NewClient(endpoint string) *Client {
	return &Client{endpoint: endpoint, httpc: &http.Client{Timeout: 2 * time.Second}}
}

func (c *Client) Call(ctx context.Context, method string, args any) (any, error) {
	c.seq++
	params, _ := json.Marshal(args)
	body, _ := json.Marshal(request{ID: c.seq, Method: method, Params: params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rpc 传输失败: %w", err)
	}
	defer res.Body.Close()
	var out response
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("rpc 解码失败: %w", err)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("rpc 业务错误: %s", out.Error)
	}
	return out.Result, nil
}

func main() {
	// 慢服务：3 秒才回；客户端只等 200 毫秒，用来演示 deadline 生效。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		_ = json.NewEncoder(w).Encode(response{Result: 7})
	}))
	defer ts.Close()

	c := NewClient(ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	result, err := c.Call(ctx, "Add", struct{ A, B int }{3, 4})
	fmt.Printf("结果: %v, 错误: %v\n", result, err)
}
