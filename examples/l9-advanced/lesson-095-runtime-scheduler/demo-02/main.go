package main

import (
	"fmt"
	"runtime"
	"time"
)

// 观察二：无函数调用的紧循环占满单个 P，验证 sysmon 的异步抢占。
// Go 1.14 起，纯计算循环也能被抢占，main 不会被永久卡住。
var sink int64

func main() {
	runtime.GOMAXPROCS(1)
	done := make(chan struct{})
	go func() {
		var x int64
		for i := int64(0); i < 2_000_000_000; i++ {
			x += i % 7
		}
		sink = x
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("后台循环已结束")
	case <-time.After(100 * time.Millisecond):
		fmt.Println("100ms 内 main 仍被调度：异步抢占生效")
		fmt.Println("P 数量 =", runtime.GOMAXPROCS(0))
	}
}
