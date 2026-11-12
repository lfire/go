package main

import (
	"errors"
	"fmt"
)

func divide(a int, b int) (int, error) { // 结果在前，错误在后
	if b == 0 {
		return 0, errors.New("除数不能为零")
	}
	return a / b, nil
}

func main() {
	q, err := divide(42, 2)
	if err != nil {
		fmt.Println("出错了")
		return
	}
	fmt.Println("商：", q)
	q2, _ := divide(10, 3) // 用空标识符忽略不需要的值
	fmt.Println("只取商：", q2)
}
