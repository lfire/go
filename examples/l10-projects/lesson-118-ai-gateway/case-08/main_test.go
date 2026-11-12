// 文件：internal/provider/adapters_test.go
package main

import (
	"context"
	"strings"
	"testing"
)

type Chunk struct {
	Text string
	Done bool
}

// parse 是被测的极简归一化：取 data 行文本，[DONE] 结束。
func parse(body string, emit func(Chunk) error) error {
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		d := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		switch {
		case d == "[DONE]":
			if err := emit(Chunk{Done: true}); err != nil {
				return err
			}
		case strings.Contains(d, "text"):
			if err := emit(Chunk{Text: d}); err != nil {
				return err
			}
		}
	}
	return nil
}

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		body string
		text string
		done bool
	}{
		{"单块加结束", "data: text=A\n\ndata: [DONE]\n\n", "text=A", true},
		{"多块拼接", "data: text=你\n\ndata: text=好\n\n", "text=你text=好", false},
		{"空流", "event: ping\n\n", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got string
			var done bool
			if err := parse(c.body, func(ch Chunk) error {
				got += ch.Text
				done = done || ch.Done
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if got != c.text || done != c.done {
				t.Fatalf("got=%q done=%v", got, done)
			}
		})
	}
}

func TestCancelStopsStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := parseWithContext(ctx, "data: text=x\n\n", func(Chunk) error { return nil })
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

func parseWithContext(ctx context.Context, body string, emit func(Chunk) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return parse(body, func(c Chunk) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		return emit(c)
	})
}

func BenchmarkParse(b *testing.B) {
	body := "data: text=你\n\ndata: text=好\n\ndata: [DONE]\n\n"
	for i := 0; i < b.N; i++ {
		_ = parse(body, func(Chunk) error { return nil })
	}
}
