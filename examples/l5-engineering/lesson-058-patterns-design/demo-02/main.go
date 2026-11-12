// 策略与中间件：用函数类型做可插拔的横切逻辑
package main

import (
	"fmt"
	"strings"
)

// Handler 是业务函数类型
type Handler func(string) string

// Middleware 把一个 Handler 包装成另一个 Handler
type Middleware func(Handler) Handler

func Logging(next Handler) Handler {
	return func(s string) string {
		fmt.Println("log: 收到", s)
		return next(s)
	}
}

func Trim(next Handler) Handler {
	return func(s string) string {
		return next(strings.TrimSpace(s))
	}
}

// Chain 逆序套上中间件，最靠前的在最外层
func Chain(h Handler, mws ...Middleware) Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func main() {
	business := func(s string) string {
		return "hello " + s
	}
	h := Chain(business, Logging, Trim)
	fmt.Println(h("  ada  "))
}
