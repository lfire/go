package main

import (
	"context"
	"fmt"
)

type mdKey struct{} // 自定义类型作 context key，避免跨包冲突

// Metadata 类比 grpc/metadata.MD：本质是 map[string][]string，挂在 context 上透传。
type Metadata map[string][]string

func NewOutgoingContext(ctx context.Context, md Metadata) context.Context {
	return context.WithValue(ctx, mdKey{}, md)
}
func FromIncomingContext(ctx context.Context) (Metadata, bool) {
	md, ok := ctx.Value(mdKey{}).(Metadata)
	return md, ok
}
func (m Metadata) Get(k string) string {
	if vs := m[k]; len(vs) > 0 {
		return vs[0]
	}
	return ""
}

// Status 类比 grpc/status：把错误码与消息做成可判定的 error。
type Code int

const (
	OK Code = iota
	InvalidArgument
	Unauthenticated
	DeadlineExceeded
	Internal
)

type Status struct {
	Code Code
	Msg  string
}

func (s *Status) Error() string { return fmt.Sprintf("rpc error: code=%d msg=%s", s.Code, s.Msg) }

func Errorf(c Code, format string, a ...any) error {
	return &Status{Code: c, Msg: fmt.Sprintf(format, a...)}
}

// CodeOf 让调用方按码判定；非 status 错误一律归为 Internal，避免泄露内部细节。
func CodeOf(err error) Code {
	if s, ok := err.(*Status); ok {
		return s.Code
	}
	return Internal
}

func main() {
	// 客户端把 trace-id 与鉴权信息塞进 metadata 发出
	ctx := NewOutgoingContext(context.Background(), Metadata{
		"trace-id":      {"t-9527"},
		"authorization": {"Bearer abc"},
	})

	// 服务端逐跳提取；要继续调用下游，还得再挂回 Outgoing context
	md, _ := FromIncomingContext(ctx)
	fmt.Println("透传 trace-id:", md.Get("trace-id"))

	err := Errorf(Unauthenticated, "token 已过期")
	fmt.Printf("错误码=%d 可判定=%v\n", CodeOf(err), CodeOf(err) == Unauthenticated)
}
