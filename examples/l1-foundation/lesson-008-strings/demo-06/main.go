package main

import (
	"fmt"
	"unicode/utf8"
)

func main() {
	s := "Go中文"
	bad := s[:3]                  // 按字节切，切在汉字中间
	good := string([]rune(s)[:3]) // 按字符切，得到 Go中
	fmt.Println("字节切片是合法 UTF-8 吗：", utf8.ValidString(bad))
	fmt.Println("按字符切的结果：", good)
}
