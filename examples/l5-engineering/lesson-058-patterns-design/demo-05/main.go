package main

import (
	"fmt"
	"time"
)

type Server struct{ timeout time.Duration }

type Option func(*Server)

func WithTimeout(d time.Duration) Option {
	return func(s *Server) { s.timeout = d }
}

func New(opts ...Option) *Server {
	s := &Server{timeout: 3 * time.Second}
	for _, o := range opts {
		o(s)
	}
	return s
}

func main() {
	_ = New(WithTimeout(5 * time.Second))
	fmt.Println("ok")
}
