package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	var total atomic.Int64 // 零值即可用
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				total.Add(1) // 原子加，无需互斥锁
			}
		}()
	}
	wg.Wait()
	fmt.Println("total =", total.Load())

	var limit atomic.Int64
	limit.Store(3)
	ok := limit.CompareAndSwap(3, 5) // 当前是 3，成功
	fmt.Println("CAS 成功 =", ok, "新值 =", limit.Load())
	fail := limit.CompareAndSwap(3, 9) // 期望已不符，失败
	fmt.Println("CAS 失败 =", fail, "值仍为 =", limit.Load())
}
