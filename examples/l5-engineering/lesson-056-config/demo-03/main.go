// 热加载：用 atomic.Value 原子替换整份配置，读侧无锁
package main

import (
	"fmt"
	"sync/atomic"
)

type Config struct {
	RateLimit int
	GrayPct   int
}

// holder 保存当前生效的一份配置，写入是整体替换
var holder atomic.Value

func init() {
	holder.Store(Config{RateLimit: 100, GrayPct: 0})
}

// Reload 从远端拉到新值后整体替换，读侧永远看到完整一份
func Reload(c Config) {
	holder.Store(c)
}

func Current() Config {
	return holder.Load().(Config)
}

func main() {
	fmt.Println("初始:", Current())
	// 模拟一次远端推送：灰度比例从 0 放到 10
	Reload(Config{RateLimit: 200, GrayPct: 10})
	fmt.Println("热更新后:", Current())
}
