package main

import "fmt"

func main() {
	s := "中文"
	for i := 0; i < len(s); i++ {
		fmt.Printf("字节 %d：%x\n", i, s[i])
	}
}
