// cmd/api/main.go 的等价示例：入口只做装配，协议层只认业务抽象
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
)

// Query 是业务层对外暴露的能力
type Query interface {
	Total(id string) (int, error)
}

// Handler 属于协议层，依赖的是业务抽象而非具体实现
type Handler struct{ q Query }

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	v, err := h.q.Total("A-1001")
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	fmt.Fprintln(w, "订单合计:", v)
}

// fakeQuery 是装配时注入的实现，真实项目里来自 internal/service
type fakeQuery struct{}

func (fakeQuery) Total(id string) (int, error) { return 280, nil }

func main() {
	// 唯一知道「谁实现谁」的地方：入口组装
	h := Handler{q: fakeQuery{}}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	fmt.Println("状态码:", rec.Code)
	fmt.Println("响应:", rec.Body.String())
}
