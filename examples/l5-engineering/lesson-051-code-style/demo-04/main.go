package main

import "fmt"

// logf 是自定义的 printf-like 包装，go vet 默认不认识它。
func logf(format string, args ...any) {
	fmt.Printf("[app] "+format+"\n", args...)
}

func main() {
	fmt.Printf("%d", "x") // 常量格式串：动词与参数静态可判
	format := "%d"
	fmt.Printf(format, "x") // 格式串是变量：静态形状里没有常量
	logf("%d", "x")         // 自定义包装：未注册时 vet 不检查
}
