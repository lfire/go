package main

import (
	"fmt"
	"runtime"
)

//go:generate echo 真实项目常在这里调用 stringer 之类的生成器

func main() {
	fmt.Println("构建目标：", runtime.GOOS, runtime.GOARCH)
}
