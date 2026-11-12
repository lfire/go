package main

import (
	"errors"
	"fmt"
)

// ErrEmptyName 是包级「哨兵错误」：预先创建好、供全包复用的错误值。
var ErrEmptyName = errors.New("name 不能为空")

// greet 把失败作为第二个返回值交还给调用方，而不是中断程序。
func greet(name string) (string, error) {
	if name == "" {
		return "", ErrEmptyName
	}
	return "你好，" + name, nil
}

func main() {
	s, err := greet("Gopher")
	fmt.Println(s)                   // 你好，Gopher
	fmt.Println(err == nil)          // true：没有失败时 error 为 nil

	_, err = greet("")
	fmt.Println(err != nil)          // true：非 nil 即代表失败
	fmt.Println(err == ErrEmptyName) // true：同一个哨兵值可以直接比较
}
