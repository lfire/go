package main

import (
	"fmt"
	"os"
)

// defer 数量固定、无循环、无 recover：编译期可完全展开
func writeFile(path string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close() // 第 1 条：关闭文件
	defer func() {  // 第 2 条：出错时补一条日志
		if err != nil {
			fmt.Println("写文件出错:", err)
		}
	}()
	_, err = f.WriteString("hello go\n")
	return err
}

func main() {
	if err := writeFile("demo.txt"); err != nil {
		fmt.Println("失败:", err)
	}
}
