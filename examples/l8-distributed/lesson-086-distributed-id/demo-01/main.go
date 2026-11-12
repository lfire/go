package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// UUIDv4 用 16 字节随机数生成一个全局唯一标识
func UUIDv4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // 版本号 4
	b[8] = (b[8] & 0x3f) | 0x80 // 变体位
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func main() {
	for i := 0; i < 3; i++ {
		fmt.Println("UUID:", UUIDv4())
	}
	// 自增 ID 天然有序，插入 B+ 树时总落在最右侧页
	for i := 1; i <= 3; i++ {
		fmt.Println("自增 ID:", i)
	}
}
