package main

import "fmt"

func snapshot() {
	x := 1
	defer fmt.Println("快照 =", x)
	x = 9
	fmt.Println("函数内 x =", x)
}

func closure() {
	x := 1
	defer func() { fmt.Println("闭包读到 =", x) }()
	x = 9
}

func protect(job func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("已接住: %v", r)
		}
	}()
	job()
	return nil
}

func main() {
	snapshot()
	closure()
	fmt.Println("protect 返回 =", protect(func() { panic("磁盘不可读") }))
	fmt.Println("protect 正常 =", protect(func() {}) == nil)
}
