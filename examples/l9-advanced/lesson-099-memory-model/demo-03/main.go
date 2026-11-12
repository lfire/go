package main

import (
	"fmt"
	"sync"
)

// 观察三：channel、Mutex、Once 分别建立 synchronizes-before 边。
func main() {
	ch := make(chan int)
	x := 0
	go func() {
		x = 42
		ch <- 1
	}()
	<-ch
	fmt.Println("channel 保证 x =", x)

	var mu sync.Mutex
	y := 0
	mu.Lock()
	y = 7
	mu.Unlock()
	mu.Lock()
	fmt.Println("Mutex 保证 y =", y)
	mu.Unlock()

	var once sync.Once
	z := 0
	once.Do(func() { z = 9 })
	fmt.Println("Once 保证 z =", z)
}
