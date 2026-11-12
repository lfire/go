package main

import (
	"fmt"
	"runtime"
)

func main() {
	fmt.Println("逻辑 CPU 数:", runtime.NumCPU())
	fmt.Println("GOMAXPROCS:", runtime.GOMAXPROCS(0)) // 传 0 只查询，不修改
	fmt.Println("当前 goroutine 数:", runtime.NumGoroutine())

	old := runtime.GOMAXPROCS(2) // 设为 2，并返回设置前的旧值
	fmt.Println("设置前的旧值:", old)
	fmt.Println("设置后:", runtime.GOMAXPROCS(0))
	runtime.GOMAXPROCS(old) // 恢复原值，避免影响后续代码
}
