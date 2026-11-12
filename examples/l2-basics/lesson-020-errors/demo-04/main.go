package main

import (
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("not found")

func load(id string) error {
	return fmt.Errorf("加载 %s: %w", id, ErrNotFound)
}

func main() {
	err := load("u1")
	fmt.Println("等值比较 =", err == ErrNotFound)
	fmt.Println("errors.Is =", errors.Is(err, ErrNotFound))
	fmt.Println("errors.Unwrap =", errors.Unwrap(err))

	joined := errors.Join(ErrNotFound, errors.New("timeout"))
	fmt.Println("Is 命中子树 =", errors.Is(joined, ErrNotFound))
	fmt.Println("Unwrap 对 Join 返回 nil =", errors.Unwrap(joined) == nil)

	fmt.Println("链上文本 =", err.Error())
}
