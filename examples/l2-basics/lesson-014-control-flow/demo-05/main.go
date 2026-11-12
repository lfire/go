package main

import (
	"fmt"
	"strconv"
)

func main() {
	// if 初始化语句最典型的用途：先取值和错误，再按错误分支
	if n, err := strconv.Atoi("42"); err == nil {
		fmt.Println("解析成功，n =", n)
	} else {
		fmt.Println("解析失败，n 取零值 =", n)
	}

	// 错误分支里 n、err 依然可见；出了整条链就失效
	if n, err := strconv.Atoi("abc"); err != nil {
		fmt.Println("非法输入被挡住，n =", n, "有错 =", err != nil)
	}
}
