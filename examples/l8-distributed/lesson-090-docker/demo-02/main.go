package main

import (
	"fmt"
	"os"
)

// 这些变量由 go build -ldflags "-X main.Version=..." 注入
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func main() {
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "production"
	}
	fmt.Println("版本:", Version)
	fmt.Println("提交:", Commit)
	fmt.Println("构建时间:", BuildTime)
	fmt.Println("运行环境:", env)
}
