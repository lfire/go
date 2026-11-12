package main

import (
	"fmt"
	"strings"
)

func main() {
	h := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	parts := strings.Split(h, "-")
	fmt.Printf("版本=%s TraceID长度=%d SpanID长度=%d 采样=%s\n",
		parts[0], len(parts[1]), len(parts[2]), parts[3])
}
