package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Hook 是框架内建的可观测钩子，请求开始与结束各调一次。
type Hook func(ctx context.Context, method string)

type Server struct {
	mu       sync.Mutex
	started  bool
	services []string
	before   []Hook
	after    []Hook
}

func (s *Server) Use(before, after Hook) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.before = append(s.before, before)
	s.after = append(s.after, after)
}

// Start 统一做注册，业务 handler 之前由框架把横切能力装好。
func (s *Server) Start(registrar func(string)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return fmt.Errorf("already started")
	}
	for _, name := range s.services {
		registrar(name) // 对接第 075 讲的注册中心
	}
	s.started = true
	return nil
}

// Stop 先反注册再停止接流量，这是优雅退出的关键顺序。
func (s *Server) Stop(deregister func(string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return
	}
	for _, name := range s.services {
		deregister(name)
	}
	s.started = false
}

func (s *Server) Invoke(ctx context.Context, method string) {
	for _, h := range s.before {
		h(ctx, method) // 日志、指标、trace 注入都在这里
	}
	// 这里是业务 handler 的位置
	for _, h := range s.after {
		h(ctx, method)
	}
}

func main() {
	s := &Server{services: []string{"order.Greeter"}}
	s.Use(
		func(_ context.Context, m string) { fmt.Println("before:", m) },
		func(_ context.Context, m string) { fmt.Println("after :", m) },
	)
	registered := []string{}
	_ = s.Start(func(n string) { registered = append(registered, n) })
	fmt.Println("已注册:", registered)
	s.Invoke(context.Background(), "SayHello")

	// 真实框架用 signal.NotifyContext 阻塞等待 SIGINT/SIGTERM；这里用超时让演示自动结束
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
		fmt.Println("收到退出信号")
	case <-time.After(50 * time.Millisecond):
		fmt.Println("演示：未等到信号，主动走停机流程")
	}
	s.Stop(func(n string) { fmt.Println("反注册:", n) })
	fmt.Println("已停止")
}
