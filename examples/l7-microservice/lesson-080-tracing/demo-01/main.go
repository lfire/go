package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type spanKey struct{}

// Span 是一次操作的最小追踪单元，归属某个 Trace，并记住父 Span。
type Span struct {
	TraceID    string
	SpanID     string
	ParentID   string
	Name       string
	Start      time.Time
	Attributes map[string]string
}

func newID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// StartSpan 从 ctx 取父 Span，有则继承 TraceID，无则开一条新 Trace。
func StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	parent, _ := ctx.Value(spanKey{}).(*Span)
	s := &Span{SpanID: newID(8), Name: name, Start: time.Now(), Attributes: map[string]string{}}
	if parent != nil {
		s.TraceID = parent.TraceID
		s.ParentID = parent.SpanID
	} else {
		s.TraceID = newID(16)
	}
	return context.WithValue(ctx, spanKey{}, s), s
}

func (s *Span) End() { /* 真实实现会上报耗时与属性 */ }

func main() {
	ctx, root := StartSpan(context.Background(), "gateway")
	_, db := StartSpan(ctx, "db.query")
	_, cache := StartSpan(ctx, "cache.get")
	fmt.Println("同一 TraceID:", root.TraceID == db.TraceID && db.TraceID == cache.TraceID)
	fmt.Println("db 父 Span:", db.ParentID == root.SpanID)
	fmt.Println("TraceID 长度:", len(root.TraceID), "SpanID 长度:", len(db.SpanID))
}
