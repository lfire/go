package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	ch := make(chan int) // 无缓冲：没有槽位，接收者只能排进 recvq
	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			v := <-ch // 三个 G 都挂起，登记进 recvq
			fmt.Printf("接收者 %d 被唤醒，拿到 %d\n", id, v)
		}(i)
	}
	time.Sleep(10 * time.Millisecond) // 确保三个接收者都已入队
	fmt.Println("此时 recvq 里排着 3 个等待的 G")
	ch <- 100 // 每次发送从 recvq 队首取一个 sudog，直接拷贝并唤醒
	ch <- 200
	ch <- 300
	wg.Wait()
	fmt.Println("全部接收者完成")
}
