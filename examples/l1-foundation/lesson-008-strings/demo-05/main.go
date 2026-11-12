package main

import (
	"fmt"
	"strings"
)

func main() {
	var b strings.Builder
	for i := 0; i < 3; i++ {
		b.WriteString("Go") // 只往缓冲区追加，不产生临时字符串
	}
	fmt.Println("拼接结果：", b.String())
	fmt.Println("缓冲长度：", b.Len())
}
