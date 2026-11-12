package main

import (
	"fmt"
	"os"
)

func main() {
	proxy := os.Getenv("GOPROXY") // 读取进程环境变量
	if proxy == "" {
		proxy = "未显式设置，使用默认值"
	}
	fmt.Println("当前 GOPROXY：", proxy)
}
