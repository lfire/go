// Functional Options：把可选配置收敛成一组函数
package main

import (
	"fmt"
	"time"
)

type Server struct {
	addr    string
	timeout time.Duration
	maxConn int
}

// Option 是修改 Server 配置的函数类型
type Option func(*Server)

func WithAddr(s string) Option {
	return func(sv *Server) { sv.addr = s }
}

func WithTimeout(d time.Duration) Option {
	return func(sv *Server) { sv.timeout = d }
}

func WithMaxConn(n int) Option {
	return func(sv *Server) { sv.maxConn = n }
}

// New 先给默认值，再依次应用 option
func New(opts ...Option) *Server {
	sv := &Server{addr: ":8080", timeout: 3 * time.Second, maxConn: 100}
	for _, opt := range opts {
		opt(sv)
	}
	return sv
}

func main() {
	sv := New(WithAddr(":9090"), WithTimeout(5*time.Second), WithMaxConn(200))
	fmt.Println(sv.addr, sv.timeout, sv.maxConn)
}
