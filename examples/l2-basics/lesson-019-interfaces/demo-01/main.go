package main

import "fmt"

type Writer interface {
	Write(p []byte) (int, error)
}

type Console struct{}

// 签名一致即自动实现
func (Console) Write(p []byte) (int, error) {
	fmt.Println("写入:", string(p))
	return len(p), nil
}

var _ Writer = Console{}

func save(w Writer, data string) {
	_, _ = w.Write([]byte(data))
}

func main() {
	save(Console{}, "hello")
}
