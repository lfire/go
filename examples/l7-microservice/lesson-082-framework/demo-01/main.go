package main

import (
	"errors"
	"fmt"
	"time"
)

// Error 是框架统一错误：稳定业务码、可展示消息、可包裹的底层错误。
type Error struct {
	Code    int
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("code=%d msg=%s cause=%v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("code=%d msg=%s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

func CodeOf(err error) int {
	var e *Error
	if errors.As(err, &e) { // 沿错误链判定，跨多层包装仍有效
		return e.Code
	}
	return 500
}

// Options 用函数式选项装配，调用顺序无关，新增能力只需加一个 WithXxx。
type Options struct {
	Name     string
	Addr     string
	Timeout  time.Duration
	Registry string
	Tracing  bool
	Metrics  bool
}

type Option func(*Options)

func WithAddr(a string) Option              { return func(o *Options) { o.Addr = a } }
func WithTimeout(d time.Duration) Option    { return func(o *Options) { o.Timeout = d } }
func WithRegistry(r string) Option          { return func(o *Options) { o.Registry = r } }
func WithObservability(t, m bool) Option    { return func(o *Options) { o.Tracing, o.Metrics = t, m } }

func defaultOptions() Options {
	return Options{Name: "app", Addr: ":8080", Timeout: 2 * time.Second, Tracing: true, Metrics: true}
}

type App struct{ opts Options }

func New(opts ...Option) *App {
	o := defaultOptions()
	for _, apply := range opts {
		apply(&o) // 逐个应用；默认值保证不传也能跑
	}
	return &App{opts: o}
}

func main() {
	a := New(WithAddr(":9090"), WithTimeout(500*time.Millisecond),
		WithRegistry("etcd"), WithObservability(true, true))
	fmt.Printf("装配结果: name=%s addr=%s timeout=%v registry=%s tracing=%v metrics=%v\n",
		a.opts.Name, a.opts.Addr, a.opts.Timeout, a.opts.Registry, a.opts.Tracing, a.opts.Metrics)

	err := &Error{Code: 404, Message: "order not found", Cause: fmt.Errorf("db: no rows")}
	fmt.Println("统一错误:", err)
	fmt.Println("错误码:", CodeOf(fmt.Errorf("wrap: %w", err)))
}
