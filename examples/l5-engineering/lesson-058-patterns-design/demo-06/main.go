package main

import (
	"fmt"
	"sync"
)

var calls int

func main() {
	var o sync.Once
	for i := 1; i <= 3; i++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("第 %d 次 Do: panic 被捕获 = %v\n", i, r)
				}
			}()
			o.Do(func() { calls++; panic("init boom") })
		}()
	}
	fmt.Println("f 实际执行次数 =", calls)
}
