// 文件：cmd/frameprobe/main.go（独立程序，可直接 go run）
package main

import (
	"encoding/binary"
	"fmt"
)

// 演示粘包：两个帧拼成一段字节流，只有按长度前缀切分才能还原边界。
// 此程序不依赖本讲库，可单独 go run 验证半包与毒包防护的思路。
func main() {
	frame := func(key, val string, reqID uint32) []byte {
		body := make([]byte, 0, 2+len(key)+2+len(val))
		kl := make([]byte, 2)
		binary.BigEndian.PutUint16(kl, uint16(len(key)))
		body = append(body, kl...)
		body = append(body, key...)
		body = append(body, 0, 0) // found, errCode
		body = append(body, val...)
		head := make([]byte, 12)
		head[0], head[1], head[2], head[3] = 0x4B, 1, 2, 0
		binary.BigEndian.PutUint32(head[4:8], uint32(len(body)))
		binary.BigEndian.PutUint32(head[8:12], reqID)
		return append(head, body...)
	}
	stream := append(frame("a", "1", 1), frame("b", "2", 2)...)
	fmt.Printf("字节流总长 %d，却含两帧\n", len(stream))
	off := 0
	for off+12 <= len(stream) {
		n := int(binary.BigEndian.Uint32(stream[off+4 : off+8]))
		if n > 16<<20 {
			fmt.Println("毒包：声明长度超上限，丢弃")
			return
		}
		if off+12+n > len(stream) {
			fmt.Println("半包：剩余字节不足一帧，等待更多数据")
			return
		}
		kl := int(binary.BigEndian.Uint16(stream[off+12 : off+14]))
		key := string(stream[off+14 : off+14+kl])
		fmt.Printf("还原一帧: key=%s bodyLen=%d\n", key, n)
		off += 12 + n
	}
}
