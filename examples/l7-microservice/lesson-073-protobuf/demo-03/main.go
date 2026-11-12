package main

import (
	"context"
	"fmt"
)

// 模拟 protoc 生成的桩接口：契约由 .proto 唯一确定，客户端与服务端共享同一份接口。
type GreeterServer interface {
	SayHello(ctx context.Context, name string) (string, error)
}

// 生成器还会产出 Unimplemented，业务嵌入它即可在 proto 加方法时不被编译打断。
type UnimplementedGreeterServer struct{}

func (UnimplementedGreeterServer) SayHello(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("method SayHello not implemented")
}

type service struct{ UnimplementedGreeterServer }

func (service) SayHello(_ context.Context, name string) (string, error) {
	return "hello, " + name, nil
}

// dispatcher 模拟 grpc.RegisterGreeterServer：把实现挂到按方法名分发的位置。
type dispatcher struct{ srv GreeterServer }

func (d *dispatcher) handle(ctx context.Context, method, name string) (string, error) {
	if method != "SayHello" {
		return "", fmt.Errorf("unknown method")
	}
	return d.srv.SayHello(ctx, name)
}

func main() {
	d := &dispatcher{srv: service{}}
	out, err := d.handle(context.Background(), "SayHello", "gopher")
	fmt.Printf("生成桩调用结果: out=%s err=%v\n", out, err)
}
