package main

import "fmt"

// safeCall 在 defer 中 recover，把意外转成普通返回值。
func safeCall(job func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("任务中断: %v", r)
		}
	}()
	job()
	return nil
}

func risky() {
	fmt.Println("开始执行")
	panic("磁盘不可读")
}

func main() {
	err := safeCall(risky)
	fmt.Println(err != nil) // true
	fmt.Println("主流程继续运行")
}
