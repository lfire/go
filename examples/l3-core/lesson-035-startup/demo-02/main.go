package main

import (
	"fmt"
	"sort"
)

// 包级注册表：在 init 之前就已分配好
var drivers = map[string]func() string{}

func register(name string, f func() string) { drivers[name] = f }

// init 里只做登记，不做 I/O、不启动后台任务
func init() {
	register("json", func() string { return "JSON 编解码器" })
	register("yaml", func() string { return "YAML 编解码器" })
}

func init() {
	if len(drivers) == 0 { // 启动时做一次不变量校验，失败就早失败
		panic("没有任何驱动被注册")
	}
}

func main() {
	names := make([]string, 0, len(drivers))
	for n := range drivers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Printf("驱动 %-5s → %s\n", n, drivers[n]())
	}
}
