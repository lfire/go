package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	// WithCancel：由调用方决定何时取消
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel() // 调用后 Done 被关闭
	}()
	<-ctx.Done()
	fmt.Println("WithCancel 原因：", ctx.Err())

	// WithTimeout：相对超时
	tctx, tcancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer tcancel() // 必须调用，及时释放计时器
	<-tctx.Done()
	fmt.Println("WithTimeout 原因：", tctx.Err())

	// WithDeadline：绝对时间点
	dctx, dcancel := context.WithDeadline(context.Background(), time.Now().Add(50*time.Millisecond))
	defer dcancel()
	if _, ok := dctx.Deadline(); ok {
		fmt.Println("WithDeadline 设置了截止时间：true")
	}
	<-dctx.Done()
	fmt.Println("WithDeadline 原因：", dctx.Err())
}
