package main

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"time"
)

// 反例：Sleep 与 Gosched 都不建立 synchronized-before。
// 单独用 go run -race 运行 wrong() 会报告数据竞争。
func wrong() {
	var data int
	var ready bool
	go func() {
		data = 42
		ready = true
	}()
	for i := 0; i < 1000 && !ready; i++ {
		runtime.Gosched()
	}
	time.Sleep(time.Millisecond)
	fmt.Println("wrong 读到 data =", data)
}

func right() {
	var data int
	var ready atomic.Int32
	go func() {
		data = 42
		ready.Store(1)
	}()
	for ready.Load() == 0 {
		runtime.Gosched()
	}
	fmt.Println("right 读到 data =", data)
}

func main() {
	wrong()
	right()
}
