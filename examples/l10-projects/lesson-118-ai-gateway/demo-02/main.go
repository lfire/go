// 文件：internal/provider/sse_parser.go
package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Chunk 是统一块类型，本段只用到文本与结束标志。
type Chunk struct {
	Text string
	Done bool
}

// SSEParser 逐行扫描上游 SSE，只提取 data 行并交给回调。
func SSEParser(r io.Reader, onData func(string) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), 1048576) // 放大到 1 MiB，防长 token 被截断
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // 忽略 event、id、retry 与心跳注释行
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if err := onData(payload); err != nil {
			return err // 回调报错立刻停止，取消由此反向传导
		}
	}
	if err := sc.Err(); err != nil {
		return err // 区分正常结束与读错误
	}
	return nil
}

func main() {
	up := strings.NewReader("event: ping\ndata: {\"t\":1}\n\ndata: [DONE]\n\n")
	var frames []string
	err := SSEParser(up, func(d string) error {
		frames = append(frames, d)
		return nil
	})
	fmt.Println("frames:", len(frames), "err is nil:", err == nil)
}
