package main

import (
	"fmt"
	"os"
)

// 观察二：包级变量、init、main 依次执行，命令行参数在 main 之前已就绪。
var stage = record("包级变量初始化")

func record(s string) string {
	fmt.Println(s)
	return s
}

func init() {
	fmt.Println("init 函数")
}

func main() {
	fmt.Println("main 函数")
	fmt.Println("参数个数 =", len(os.Args))
	fmt.Println("首个参数（可执行文件路径）=", os.Args[0])
}
