package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
)

var calls int

func expensive() string {
	calls++
	return "v"
}

func main() {
	h := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo})
	log := slog.New(h)
	ctx := context.Background()

	calls = 0
	log.Debug("高频", "v", expensive())
	fmt.Println("Debug 被过滤后 expensive 调用次数:", calls)

	calls = 0
	if log.Enabled(ctx, slog.LevelDebug) {
		log.Debug("高频", "v", expensive())
	}
	fmt.Println("加 Enabled 守卫后 expensive 调用次数:", calls)

	a := testing.AllocsPerRun(2000, func() {
		log.Debug("高频", "i", 1)
	})
	b := testing.AllocsPerRun(2000, func() {
		log.Debug("高频", "i", fmt.Sprintf("%d", 1))
	})
	c := testing.AllocsPerRun(2000, func() {
		if log.Enabled(ctx, slog.LevelDebug) {
			log.Debug("高频", "i", fmt.Sprintf("%d", 1))
		}
	})
	fmt.Printf("被过滤的结构化字段 allocs/op     = %.0f\n", a)
	fmt.Printf("被过滤但预先 Sprintf allocs/op  = %.0f\n", b)
	fmt.Printf("Enabled 守卫 + Sprintf allocs/op = %.0f\n", c)
}
