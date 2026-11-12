package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	dir, err := os.Getwd() // 取当前工作目录
	if err != nil {
		fmt.Println("取工作目录失败：", err)
		return
	}
	fmt.Println("当前目录名：", filepath.Base(dir)) // 只取最后一段
}
