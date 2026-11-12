// 可测试性设计：让函数只依赖参数，不偷偷读全局状态
package main

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Greet 从任意 io.Reader 读名字，时间由调用方传入，结果完全可预测。
func Greet(r io.Reader, now time.Time) (string, error) {
	name, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("读取姓名: %w", err)
	}
	hour := now.Hour()
	return fmt.Sprintf("%s，现在 %d 点", strings.TrimSpace(string(name)), hour), nil
}

func main() {
	t := time.Date(2024, 1, 2, 9, 0, 0, 0, time.UTC)
	s, err := Greet(strings.NewReader("ada"), t)
	if err != nil {
		fmt.Println("失败:", err)
		return
	}
	fmt.Println(s)
}
