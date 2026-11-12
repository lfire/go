package main

import "fmt"

type Speaker interface {
	Speak() string
}

type Dog struct{}

func (d Dog) Speak() string { return "wang" }

type Cat struct{}

func (c *Cat) Speak() string { return "miao" }

func main() {
	var s Speaker
	s = Dog{} // 值类型即满足接口
	fmt.Println(s.Speak())

	s = &Cat{} // 只有指针满足
	fmt.Println(s.Speak())

	// s = Cat{} 会编译失败：Cat 的方法集不含 Speak
	var _ Speaker = (*Cat)(nil) // 编译期断言
}
