package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

// Observability 是框架内建聚合：计数、错误数、累计耗时，业务零感知。
type Observability struct {
	requests atomic.Uint64
	errors   atomic.Uint64
	latency  atomic.Int64 // 累计纳秒
}

func (o *Observability) Report() (uint64, uint64, time.Duration) {
	return o.requests.Load(), o.errors.Load(), time.Duration(o.latency.Load())
}

// Middleware 是统一拦截链签名，与第 74 讲一致。
type Middleware func(ctx context.Context, method string, next func() error) error

func chain(ms []Middleware, final func() error) func() error {
	for i := len(ms) - 1; i >= 0; i-- { // 反向包裹，数组靠前者在外层
		m := ms[i]
		next := final
		final = func() error { return m(context.Background(), "call", next) }
	}
	return final
}

func Build(obs *Observability) []Middleware {
	logging := func(_ context.Context, method string, next func() error) error {
		fmt.Println("log: 进入", method)
		return next()
	}
	tracing := func(_ context.Context, _ string, next func() error) error {
		// 真实框架在这里 Extract traceparent 并创建子 Span
		return next()
	}
	metrics := func(_ context.Context, _ string, next func() error) error {
		start := time.Now()
		err := next()
		obs.latency.Add(int64(time.Since(start)))
		obs.requests.Add(1)
		if err != nil {
			obs.errors.Add(1)
		}
		return err
	}
	return []Middleware{logging, tracing, metrics}
}

func main() {
	obs := &Observability{}
	handler := func() error { time.Sleep(10 * time.Millisecond); return nil }
	call := chain(Build(obs), handler)
	for i := 0; i < 3; i++ {
		_ = call()
	}
	req, errs, lat := obs.Report()
	fmt.Printf("内建观测: 请求=%d 错误=%d 累计耗时=%v\n", req, errs, lat)
}
