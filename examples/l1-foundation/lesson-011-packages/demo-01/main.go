package main

import (
	"fmt"
	"strings" // 导入路径就是标准库里的位置
)

func main() {
	title := "go modules"
	fmt.Println(strings.ToUpper(title))  // 通过包名访问导出函数
	fmt.Println(strings.Repeat("-", 3)) // 首字母大写才可跨包使用
}
