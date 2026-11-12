package main

import (
	"errors"
	"fmt"
)

type Reader interface {
	Read() string
}

type fileReader struct {
	data string
}

func (f *fileReader) Read() string { return f.data }

// openReader 失败时必须显式返回 nil 字面量，
// 不能返回带类型的 nil 指针，否则接口整体不为 nil。
func openReader(ok bool) (Reader, error) {
	if !ok {
		return nil, errors.New("打开失败") // 显式 nil，接口整体才为 nil
	}
	return &fileReader{data: "hello"}, nil
}

func main() {
	r, err := openReader(true)
	if err == nil {
		fmt.Println("读到 =", r.Read())
	}

	bad, err := openReader(false)
	fmt.Println("失败时接口为 nil =", bad == nil, "出错 =", err != nil)
}
