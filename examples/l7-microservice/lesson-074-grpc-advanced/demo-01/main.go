package main

import (
	"context"
	"fmt"
	"log"
	"time"
)

type MethodInfo struct{ FullMethod string }

type UnaryHandler func(ctx context.Context, req any) (any, error)
type UnaryInterceptor func(ctx context.Context, req any, info *MethodInfo, handler UnaryHandler) (any, error)

// chain 把多个拦截器反向串成洋葱：数组里越靠前越在外层，先做前置逻辑，最后才到业务。
func chain(interceptors []UnaryInterceptor, final UnaryHandler) UnaryHandler {
	for i := len(interceptors) - 1; i >= 0; i-- {
		in := interceptors[i]
		next := final
		final = func(ctx context.Context, req any) (any, error) {
			return in(ctx, req, &MethodInfo{FullMethod: "/demo.Greeter/SayHello"}, next)
		}
	}
	return final
}

func loggingInterceptor(ctx context.Context, req any, info *MethodInfo, handler UnaryHandler) (any, error) {
	start := time.Now()
	log.Printf("进入 %s", info.FullMethod)
	resp, err := handler(ctx, req) // 调用链继续向内
	log.Printf("退出 %s 耗时 %v err=%v", info.FullMethod, time.Since(start), err)
	return resp, err
}

type ctxKey string

const tokenKey ctxKey = "token"

func authInterceptor(ctx context.Context, req any, _ *MethodInfo, handler UnaryHandler) (any, error) {
	token, _ := ctx.Value(tokenKey).(string)
	if token == "" {
		return nil, fmt.Errorf("unauthenticated: 缺少 token")
	}
	return handler(ctx, req)
}

func business(_ context.Context, req any) (any, error) {
	name, _ := req.(string)
	return "hello, " + name, nil
}

func main() {
	h := chain([]UnaryInterceptor{loggingInterceptor, authInterceptor}, business)
	ctx := context.WithValue(context.Background(), tokenKey, "abc123")
	out, err := h(ctx, "gopher")
	fmt.Printf("结果: %v 错误: %v\n", out, err)

	// 不带 token 再调一次，认证层应当短路
	_, err2 := h(context.Background(), "gopher")
	fmt.Println("无 token 时:", err2)
}
