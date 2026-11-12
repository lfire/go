package main

import (
	"fmt"
	"runtime"
	"sync/atomic"
)

// 观察一：用 atomic 的 Store/Load 建立 synchronized-before，
// (1) 写 data 与 (2) Store synchronizes before (3) Load 与 (4) 读 data，
// 因此 (4) 必然看到 42。
var data int
var ready int32

func main() {
	go func() {
		data = 42
		atomic.StoreInt32(&ready, 1)
	}()
	for atomic.LoadInt32(&ready) == 0 {
		runtime.Gosched()
	}
	fmt.Println("data =", data)
}
