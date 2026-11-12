// 文件：internal/provider/adapters.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Chunk struct {
	Text string
	Done bool
}

type ChatRequest struct {
	Model    string
	Messages []string
}

type Provider interface {
	Name() string
	Stream(ctx context.Context, req ChatRequest, emit func(Chunk) error) error
}

// opener 发起上游请求并返回原始 SSE 响应体，由具体供应商实现。
type opener func(ctx context.Context, req ChatRequest) (io.ReadCloser, error)

// adapter 把「取流」与「解析」参数化，Stream 逻辑完全复用。
type adapter struct {
	name  string
	open  opener
	parse func(io.Reader, func(Chunk) error) error
}

var _ Provider = (*adapter)(nil)

func (a *adapter) Name() string { return a.name }

func (a *adapter) Stream(ctx context.Context, req ChatRequest, emit func(Chunk) error) error {
	body, err := a.open(ctx, req)
	if err != nil {
		return err
	}
	defer body.Close()
	return a.parse(body, emit)
}

// httpOpener 用真实 HTTP 客户端发起流式请求，ctx 取消会沿连接传到上游。
func httpOpener(endpoint, apiKey string) opener {
	return func(ctx context.Context, req ChatRequest) (io.ReadCloser, error) {
		payload, _ := json.Marshal(map[string]any{"model": req.Model, "stream": true})
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
			strings.NewReader(string(payload)))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close() // 非 200 必须关体，否则连接泄漏
			return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
		}
		return resp.Body, nil
	}
}

// parseOpenAI 把 delta.content 归一成 Chunk，[DONE] 结束。
func parseOpenAI(r io.Reader, emit func(Chunk) error) error {
	return scanData(r, func(data string) error {
		if data == "[DONE]" {
			return emit(Chunk{Done: true})
		}
		var v struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &v); err != nil {
			return nil // 单帧脏数据不掐断整条流
		}
		if len(v.Choices) > 0 && v.Choices[0].Delta.Content != "" {
			return emit(Chunk{Text: v.Choices[0].Delta.Content})
		}
		return nil
	})
}

// parseAnthropic 把 content_block_delta 与 message_stop 归一成同一个 Chunk。
func parseAnthropic(r io.Reader, emit func(Chunk) error) error {
	return scanData(r, func(data string) error {
		var v struct {
			Type  string `json:"type"`
			Delta struct {
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &v); err != nil {
			return nil
		}
		if v.Type == "content_block_delta" && v.Delta.Text != "" {
			return emit(Chunk{Text: v.Delta.Text})
		}
		if v.Type == "message_stop" {
			return emit(Chunk{Done: true})
		}
		return nil
	})
}

// scanData 是本段自带的极简 SSE 行扫描，避免依赖外部段即可独立编译。
func scanData(r io.Reader, onData func(string) error) error {
	buf := make([]byte, 4096)
	var sb strings.Builder
	for {
		n, err := r.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	for _, line := range strings.Split(sb.String(), "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if e := onData(data); e != nil {
			return e
		}
	}
	return nil
}

func staticOpener(body string) opener {
	return func(_ context.Context, _ ChatRequest) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(body)), nil
	}
}

func main() {
	openaiBody := "data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n" +
		"data: [DONE]\n\n"
	anthropicBody := "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hi\"}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	providers := []Provider{
		&adapter{name: "openai", open: staticOpener(openaiBody), parse: parseOpenAI},
		&adapter{name: "anthropic", open: staticOpener(anthropicBody), parse: parseAnthropic},
	}
	for _, p := range providers {
		var text string
		var done bool
		_ = p.Stream(context.Background(), ChatRequest{Model: "m"}, func(c Chunk) error {
			text += c.Text
			done = done || c.Done
			return nil
		})
		fmt.Println(p.Name(), text, "done:", done)
	}
}
