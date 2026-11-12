package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
)

type Request struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}
type Response struct {
	ID     uint64 `json:"id"`
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

// Server 用一张方法表把名字映射到可调用的函数值，reflect 让我们免于为每个方法写 switch。
type Server struct {
	mu      sync.RWMutex
	methods map[string]reflect.Value
}

func NewServer() *Server { return &Server{methods: make(map[string]reflect.Value)} }

func (s *Server) Register(name string, fn any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.methods[name] = reflect.ValueOf(fn)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = json.NewEncoder(w).Encode(Response{Error: "bad request"})
		return
	}
	resp := Response{ID: req.ID}
	s.mu.RLock()
	fn, ok := s.methods[req.Method]
	s.mu.RUnlock()
	if !ok {
		resp.Error = "method not found: " + req.Method
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	// 为求最小，约定注册的函数都是 func(int, int) (int, error) 形态，
	// 真实框架会用 reflect 解析入参类型并按 Params 逐个填充。
	var args struct{ A, B int }
	_ = json.Unmarshal(req.Params, &args)
	out := fn.Call([]reflect.Value{reflect.ValueOf(args.A), reflect.ValueOf(args.B)})
	if e := out[1].Interface(); e != nil {
		resp.Error = fmt.Sprint(e)
	} else {
		resp.Result = out[0].Interface()
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func Add(a, b int) (int, error) { return a + b, nil }

func main() {
	s := NewServer()
	s.Register("Add", Add)
	ts := httptest.NewServer(s)
	defer ts.Close()
	fmt.Println("RPC 服务端已就绪:", ts.URL)
}
