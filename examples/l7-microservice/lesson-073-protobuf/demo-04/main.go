package main

import (
	"encoding/hex"
	"fmt"
)

// putVarint 按 protobuf 规则编码：每字节低 7 位存数据，MSB 表示是否还有后续字节。
func putVarint(x uint64) []byte {
	var out []byte
	for x >= 0x80 {
		out = append(out, byte(x)|0x80)
		x >>= 7
	}
	return append(out, byte(x))
}

// tag 把字段号与 wire type 合成一个 varint。
func tag(field int, wire int) []byte {
	return putVarint(uint64(field)<<3 | uint64(wire))
}

func main() {
	// 字段号 1、wire type 0、值 150，官方示例编码应为 08 96 01
	msg := append(tag(1, 0), putVarint(150)...)
	fmt.Println("wire hex:", hex.EncodeToString(msg))

	// 字段号 2、wire type 2、字符串 go：tag 0x12，长度 0x02，内容 67 6f
	str := append(tag(2, 2), putVarint(2)...)
	str = append(str, []byte("go")...)
	fmt.Println("string hex:", hex.EncodeToString(str))

	// 值 300 需要两个字节，验证多字节 varint
	fmt.Println("300 varint:", hex.EncodeToString(putVarint(300)))
}
