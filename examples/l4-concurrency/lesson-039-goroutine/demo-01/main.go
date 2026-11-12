package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fmt.Println("goroutine: 我开始工作")
		time.Sleep(50 * time.Millisecond)
		fmt.Println("goroutine: 工作完成")
	}()
	fmt.Println("main: go 语句立刻返回，我不会等它")
	wg.Wait() // 删掉这一行，main 直接退出，上面两行很可能看不到
	fmt.Println("main: 结束")
}
