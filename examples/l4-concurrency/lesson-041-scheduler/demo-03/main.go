package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

func main() {
	runtime.GOMAXPROCS(1) // 只有一个 P
	start := time.Now()
	var wg sync.WaitGroup

	// G1：先启动，并留出时间让它真正进入阻塞系统调用
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = exec.Command("sleep", "0.3").Run() // 等待子进程的 wait4
	}()
	time.Sleep(50 * time.Millisecond) // 确保 G1 已经卡在内核里

	// G2：此时 P 若被 handoff，纯计算任务才能立刻跑起来
	wg.Add(1)
	go func() {
		defer wg.Done()
		sum := 0
		for i := 0; i < 20_000_000; i++ {
			sum += i
		}
		_ = sum
		fmt.Printf("G2 在 %v 处完成\n", time.Since(start).Round(time.Millisecond))
	}()

	wg.Wait()
	fmt.Printf("全部结束，共 %v\n", time.Since(start).Round(time.Millisecond))
}
