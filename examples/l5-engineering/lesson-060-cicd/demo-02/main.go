// 版本信息由构建期 -ldflags -X 注入，不写死在源码里
package main

import "fmt"

// Version 与 Commit 由 go build -ldflags 覆盖
var Version = "dev"
var Commit = "none"

func main() {
	fmt.Println("版本:", Version)
	fmt.Println("提交:", Commit)
}
