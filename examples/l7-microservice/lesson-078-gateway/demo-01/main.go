package main

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/http/httptest"
	"net/url"
	"strings"
)

// Gateway 把路径前缀映射到后端，并在转发前统一做鉴权。
type Gateway struct {
	routes map[string]*httputil.ReverseProxy
	tokens map[string]bool
}

func NewGateway() *Gateway {
	return &Gateway{
		routes: make(map[string]*httputil.ReverseProxy),
		tokens: map[string]bool{"abc123": true},
	}
}

func (g *Gateway) Register(prefix, target string) error {
	u, err := url.Parse(target)
	if err != nil {
		return err
	}
	g.routes[prefix] = httputil.NewSingleHostReverseProxy(u)
	return nil
}

func (g *Gateway) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !g.tokens[tok] {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for prefix, proxy := range g.routes {
		if strings.HasPrefix(r.URL.Path, prefix) {
			proxy.ServeHTTP(w, r)
			return
		}
	}
	http.NotFound(w, r)
}

func main() {
	// 模拟一个下游订单服务
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "order service got %s", r.URL.Path)
	}))
	defer backend.Close()

	g := NewGateway()
	_ = g.Register("/api/order", backend.URL)
	front := httptest.NewServer(g.auth(http.HandlerFunc(g.ServeHTTP)))
	defer front.Close()

	client := &http.Client{}
	resp, _ := client.Get(front.URL + "/api/order/123") // 无 token
	fmt.Println("无鉴权:", resp.StatusCode)
	resp.Body.Close()

	req, _ := http.NewRequest(http.MethodGet, front.URL+"/api/order/123", nil)
	req.Header.Set("Authorization", "Bearer abc123")
	resp2, _ := client.Do(req)
	fmt.Println("带 token:", resp2.StatusCode)
	resp2.Body.Close()
}
