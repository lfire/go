package main

import (
	"fmt"
	"runtime"
)

// 观察二：错误样例。普通 bool 的写与读之间没有 synchronized-before，
// 构成数据竞争；用 go run -race 会报告，不能用「跑几次没出错」当依据。
var data int
var ready bool

func main() {
	go func() {
		data = 42
		ready = true
	}()
	for i := 0; i < 1_000_000 && !ready; i++ {
		runtime.Gosched()
	}
	fmt.Println("data =", data)
}
