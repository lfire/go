package main // 声明这是可独立运行的程序包

import "fmt" // 导入标准库中负责格式化输出的包

func main() { // 程序入口，函数名固定、无参数、无返回值
	fmt.Println("Hello, Go!") // 打印一行文本并自动换行
}
