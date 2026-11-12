package main

import (
	"fmt"
	"sync"
)

type Config struct{ Addr string }

var (
	once sync.Once
	cfg  *Config
)

func loadConfig() *Config {
	once.Do(func() { // Do 的函数体全局只执行一次
		fmt.Println("真正执行昂贵的初始化")
		cfg = &Config{Addr: "127.0.0.1:8080"}
	})
	return cfg
}

func main() {
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1) // 在 go 之前 Add，Wait 才不提前返回
		go func(id int) {
			defer wg.Done()
			fmt.Println("协程", id, "读到", loadConfig().Addr)
		}(i)
	}
	wg.Wait()
}
