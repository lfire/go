package main

import (
	"fmt"
	"unicode/utf8"
)

func main() {
	s := "Go中文"
	fmt.Println("字节数 len：", len(s))
	fmt.Println("字符数：", utf8.RuneCountInString(s))
	fmt.Println("下标第 0 个字节：", s[0])
	for i, r := range s {
		fmt.Printf("字节偏移 %d 处是字符 %c\n", i, r)
	}
}
