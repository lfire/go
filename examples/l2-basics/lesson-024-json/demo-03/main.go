package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Event struct {
	Title string    `json:"title"`
	At    time.Time `json:"at"`
	ID    int64     `json:"id,string"` // 数字以字符串传输，避开 float64
}

func main() {
	payload := `{"title":"发布","at":"2026-01-02T15:04:05Z","id":"9007199254740993","extra":1}`
	var e Event
	dec := json.NewDecoder(strings.NewReader(payload))
	if err := dec.Decode(&e); err != nil {
		fmt.Println("解码失败:", err)
		return
	}
	fmt.Println(e.Title, e.At.UTC().Format(time.RFC3339), e.ID)

	// 流式编码：直接写进 io.Writer，不必先攒出完整字节切片。
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, title := range []string{"A", "B"} {
		_ = enc.Encode(Event{Title: title})
	}
	fmt.Print(buf.String())
}
