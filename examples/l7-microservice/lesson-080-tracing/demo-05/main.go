package main

import (
	"context"
	"fmt"
)

type spanKey struct{}

func traceID(ctx context.Context) string {
	if v, ok := ctx.Value(spanKey{}).(string); ok {
		return v
	}
	return "new-trace"
}

func main() {
	parent := context.WithValue(context.Background(), spanKey{}, "t-9527")
	fmt.Println("传 ctx:", traceID(parent))
	fmt.Println("断链:", traceID(context.Background()))
}
