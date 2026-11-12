package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
)

// Order 是本章最小 RPC 里的一条业务消息。
type Order struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

func main() {
	o := Order{ID: 150, Name: "go"}
	j, _ := json.Marshal(o)

	var buf bytes.Buffer
	_ = gob.NewEncoder(&buf).Encode(o)

	fmt.Printf("JSON: %d 字节 内容=%s\n", len(j), j)
	fmt.Printf("gob : %d 字节（含类型描述，仅 Go 可读）\n", buf.Len())
}
