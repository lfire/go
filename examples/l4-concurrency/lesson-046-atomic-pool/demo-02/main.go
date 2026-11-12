package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type Config struct {
	Level string
	Ratio int
}

func main() {
	var current atomic.Value
	current.Store(Config{Level: "info", Ratio: 10}) // 首次 Store 决定类型

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c := current.Load().(Config) // 读路径无锁
			fmt.Println("读者", id, c.Level, c.Ratio)
		}(i)
	}
	wg.Wait()
	current.Store(Config{Level: "debug", Ratio: 50}) // 整体替换
	fmt.Println("更新后：", current.Load().(Config).Level)
}
