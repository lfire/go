package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

type route struct {
	method  string
	prefix  string
	handler http.HandlerFunc
}

type miniRouter struct{ routes []route }

// Group 用闭包给子路由统一拼前缀，模拟 Gin 的 Group 实现思路。
func (m *miniRouter) Group(prefix string, register func(*miniRouter)) {
	sub := &miniRouter{}
	register(sub)
	for _, r := range sub.routes {
		r.prefix = prefix + r.prefix
		m.routes = append(m.routes, r)
	}
}

func (m *miniRouter) Handle(method, pattern string, h http.HandlerFunc) {
	m.routes = append(m.routes, route{method: method, prefix: pattern, handler: h})
}

func (m *miniRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for _, rt := range m.routes {
		if r.Method == rt.method && strings.HasPrefix(r.URL.Path, rt.prefix) {
			rt.handler(w, r)
			return
		}
	}
	http.NotFound(w, r)
}

type CreateReq struct {
	Name string `json:"name"`
}

// bind 演示「解码 + 校验」：tag 解析库只是把这里的 if 换成了反射。
func bind(r *http.Request, dst *CreateReq) error {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return err
	}
	if dst.Name == "" {
		return fmt.Errorf("name 不能为空")
	}
	return nil
}

func main() {
	m := &miniRouter{}
	m.Group("/v1", func(sub *miniRouter) {
		sub.Handle(http.MethodGet, "/users", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "list users")
		})
		sub.Handle(http.MethodPost, "/users", func(w http.ResponseWriter, r *http.Request) {
			var req CreateReq
			if err := bind(r, &req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(req)
		})
	})

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", m))
}
