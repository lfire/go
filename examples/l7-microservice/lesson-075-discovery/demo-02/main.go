package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"
)

// probe 主动探测实例健康；真实 Consul 支持 HTTP、TCP、脚本等多种检查。
func probe(client *http.Client, addr string) bool {
	resp, err := client.Get(addr + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func main() {
	// 两个后端：一个返回 200，一个返回 503 模拟业务不可用
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()

	client := &http.Client{Timeout: 500 * time.Millisecond}
	fmt.Println("健康实例 probe:", probe(client, good.URL))
	fmt.Println("故障实例 probe:", probe(client, bad.URL))
}
