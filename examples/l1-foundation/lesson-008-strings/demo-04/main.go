package main

import "fmt"

func main() {
	s := "Go"
	bs := []byte(s) // 转换会拷贝出一份可修改的字节切片
	bs[0] = 'H'     // 改的是副本，不是原串
	fmt.Println("原串：", s)
	fmt.Println("副本：", string(bs))
	t := s + "lang" // 加号拼接得到全新的字符串
	fmt.Println("拼接：", t)
}
