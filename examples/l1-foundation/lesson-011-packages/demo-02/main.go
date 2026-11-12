package main

import "fmt"

var version = "1.0" // 包级变量先完成初始化

func init() { // 包初始化时自动执行，先于 main
	version += "+init"
	fmt.Println("init 执行了")
}

func main() {
	fmt.Println("main 里的版本：", version)
}
