package main

import "fmt"

type Reader interface {
	Read() string
}

type MemReader struct{ data string }

func (m MemReader) Read() string { return m.data }

// 接受接口参数，返回具体结构体
func CopyReader(r Reader) MemReader {
	return MemReader{data: r.Read()}
}

func main() {
	got := CopyReader(MemReader{data: "hello"})
	fmt.Println("返回的具体类型 =", got.Read())

	var p *MemReader // nil 的具体指针
	var i Reader = p
	fmt.Println("i == nil ?", i == nil)

	var empty Reader
	fmt.Println("empty == nil ?", empty == nil)
}
