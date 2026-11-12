package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
)

// RPCRequest / RPCResponse 是我们自己定的线上协议。
// Method 把「调哪个函数」从 URL 搬进了消息体，于是所有方法共用一个端点。
type RPCRequest struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type RPCResponse struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error,omitempty"`
}

// 编码器的价值：把结构体与字节流的互转收敛到一个函数。
// 换 json / gob / protobuf 时，调用层一行都不用动，这就是协议与编码解耦。
func encodeJSON(v any) ([]byte, error) { return json.Marshal(v) }

func encodeGob(v any) ([]byte, error) {
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(v)
	return buf.Bytes(), err
}

func main() {
	req := RPCRequest{ID: 1, Method: "Add", Params: json.RawMessage(`{"A":3,"B":4}`)}
	b1, _ := encodeJSON(req)
	b2, _ := encodeGob(req)
	fmt.Println("JSON 报文:", string(b1))
	fmt.Printf("同样一条请求：JSON %d 字节，gob %d 字节\n", len(b1), len(b2))
}
