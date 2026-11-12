package main

import (
	"errors"
	"fmt"
	"os"
)

// openConfig 用 %w 包装：既补上下文，又不切断根因。
func openConfig(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开配置 %s: %w", path, err)
	}
	return f.Close()
}

func start() error {
	if err := openConfig("missing.yaml"); err != nil {
		return fmt.Errorf("启动服务: %w", err)
	}
	return nil
}

func main() {
	err := start()
	fmt.Println(err != nil)                     // true
	fmt.Println(errors.Is(err, os.ErrNotExist)) // true：穿透两层包装找到根因

	// errors.As 找出链上第一个 *os.PathError，取出结构化字段。
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		fmt.Println(pathErr.Path) // missing.yaml
	}
}
