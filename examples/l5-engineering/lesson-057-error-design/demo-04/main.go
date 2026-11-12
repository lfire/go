package main

import (
	"errors"
	"fmt"
)

var errNotFound = errors.New("record not found")

func main() {
	wrapped := fmt.Errorf("查询订单: %w", errNotFound)
	lost := fmt.Errorf("查询订单: %v", errNotFound)
	fmt.Println("用 %w 包装仍可判定:", errors.Is(wrapped, errNotFound))
	fmt.Println("用 %v 拼接后仍可判定:", errors.Is(lost, errNotFound))
}
