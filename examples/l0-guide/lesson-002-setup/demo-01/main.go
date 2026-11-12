package main

import (
	"fmt"
	"runtime"
)

func main() {
	fmt.Println("Go 版本：", runtime.Version())            // 运行时版本，对应 go version
	fmt.Println("系统 / 架构：", runtime.GOOS, runtime.GOARCH) // 交叉编译的两个关键变量
}
