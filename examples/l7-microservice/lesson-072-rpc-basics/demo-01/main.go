package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

// 阶段一：最朴素的 HTTP 调用——把「调用哪个方法、传什么参数」全塞进请求里。
// 调用方必须自己知道 URL、自己拼 body、自己解析响应，这就是 RPC 要消灭的样板代码。
func main() {
	// 服务端：一个 URL 对应一个方法
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/add" {
			http.NotFound(w, r)
			return
		}
		var p struct{ A, B int }
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &p)
		_ = json.NewEncoder(w).Encode(map[string]int{"result": p.A + p.B})
	}))
	defer srv.Close()

	// 客户端：手工拼 URL、手工序列化、手工反序列化
	payload := `{"A":3,"B":4}`
	resp, err := http.Post(srv.URL+"/add", "application/json", strings.NewReader(payload))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	var out map[string]int
	_ = json.NewDecoder(resp.Body).Decode(&out)
	fmt.Println("HTTP 调用结果:", out["result"])
}
