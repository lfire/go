package main

import (
	"fmt"
)

type Server struct {
	MaxConn int
}

type Option func(*Server)

func WithMaxConn(n int) Option {
	return func(s *Server) { s.MaxConn = n }
}

func (s *Server) Validate() error {
	if s.MaxConn <= 0 {
		return fmt.Errorf("MaxConn 必须为正")
	}
	return nil
}

// New 在所有 Option 应用完之后统一校验
func New(opts ...Option) (*Server, error) {
	s := &Server{MaxConn: 100}
	for _, opt := range opts {
		opt(s)
	}
	return s, s.Validate()
}

func main() {
	_, err := New(WithMaxConn(0))
	fmt.Println("校验结果:", err)
}
