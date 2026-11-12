// 内存、阻塞与锁竞争 profile：各有开关，别全量常开
package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
)

func writeHeap() error {
	f, err := os.Create("heap.pprof")
	if err != nil {
		return err
	}
	defer f.Close()
	return pprof.WriteHeapProfile(f)
}

func main() {
	// 阻塞与互斥锁采样默认关闭，按需打开，结束后复位
	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(1)
	defer runtime.SetBlockProfileRate(0)
	defer runtime.SetMutexProfileFraction(0)

	if err := writeHeap(); err != nil {
		fmt.Println("写 heap profile 失败:", err)
		return
	}
	fmt.Println("已写出 heap.pprof")
	fmt.Println("goroutine 至少为 1:", runtime.NumGoroutine() != 0)
}
