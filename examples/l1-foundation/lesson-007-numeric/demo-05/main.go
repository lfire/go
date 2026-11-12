package main

import (
	"fmt"
	"strconv"
)

func main() {
	var n int = 42
	fmt.Println("转成 int64 后相加：", int64(n)+8) // 不同整型不能直接相加
	fmt.Println("正确的整数转文本：", strconv.Itoa(n))
	var r rune = 'A' // rune 是 int32 的别名，装一个字符的码点
	fmt.Printf("码点 %d 对应的字符是 %c\n", r, r)
	fmt.Println("把码点转成文本：", string(r))
}
