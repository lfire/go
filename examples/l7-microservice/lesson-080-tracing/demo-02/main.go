package main

import (
	"fmt"
	"net/http"
	"strings"
)

const traceparent = "traceparent"

// Inject 把当前 Trace 上下文写进出站 HTTP 头，格式遵循 W3C Trace Context。
func Inject(h http.Header, traceID, spanID string, sampled bool) {
	flags := "00"
	if sampled {
		flags = "01"
	}
	h.Set(traceparent, fmt.Sprintf("00-%s-%s-%s", traceID, spanID, flags))
}

// Extract 从入站请求还原 Trace 上下文；缺头或格式不对就返回 ok=false。
func Extract(h http.Header) (traceID, spanID string, sampled, ok bool) {
	parts := strings.Split(h.Get(traceparent), "-")
	if len(parts) != 4 || parts[0] != "00" || len(parts[1]) != 32 || len(parts[2]) != 16 {
		return "", "", false, false
	}
	return parts[1], parts[2], parts[3] == "01", true
}

func main() {
	// 服务 A 出站：注入
	out := http.Header{}
	Inject(out, "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7", true)
	fmt.Println("注入头:", out.Get(traceparent))

	// 服务 B 入站：提取并继续同一条 Trace
	tid, sid, sampled, ok := Extract(out)
	fmt.Printf("提取: trace=%s parent=%s sampled=%v ok=%v\n", tid, sid, sampled, ok)

	// 完全没有头的请求，应当交给上层开新 Trace
	_, _, _, ok2 := Extract(http.Header{})
	fmt.Println("无头时 ok:", ok2)
}
