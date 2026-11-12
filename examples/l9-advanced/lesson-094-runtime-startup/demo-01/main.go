package main

import (
	"fmt"
	"runtime"
)

// 观察一：进入 main 时，运行时已把用户入口包装成 1 号 goroutine，
// 并按 CPU 核数建立好 P。这些正是 schedinit 的成果。
func main() {
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, false) // false 表示只取当前 goroutine 的栈
	fmt.Print(string(buf[:n]))

	fmt.Println("goroutine 数 =", runtime.NumGoroutine())
	fmt.Println("P 数量（GOMAXPROCS）=", runtime.GOMAXPROCS(0))
	fmt.Println("CPU 逻辑核数 =", runtime.NumCPU())
}
