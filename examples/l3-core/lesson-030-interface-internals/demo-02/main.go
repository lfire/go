package main

import (
	"bytes"
	"fmt"
	"io"
)

func describe(w io.Writer) {
	if w == nil {
		fmt.Println("真正的 nil 接口")
		return
	}
	fmt.Printf("非 nil：动态类型 %T\n", w)
}

func main() {
	var w io.Writer // 两个字段都为空
	describe(w)

	var buf *bytes.Buffer // 一个 nil 的具体指针
	describe(buf)         // 装箱后 tab 非空，接口整体不等于 nil
}
