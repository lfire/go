// 配置外置：同一份二进制，靠环境变量适配多套环境
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
)

// Config 是唯一的运行时配置结构
type Config struct {
	Addr    string
	Workers int
	Debug   bool
}

// Load 按「默认值 → flag → 环境变量」的顺序覆盖
func Load() (Config, error) {
	addr := flag.String("addr", ":8080", "监听地址")
	workers := flag.Int("workers", 4, "工作协程数")
	debug := flag.Bool("debug", false, "调试模式")
	flag.Parse()

	c := Config{Addr: *addr, Workers: *workers, Debug: *debug}
	if v, ok := os.LookupEnv("APP_ADDR"); ok {
		c.Addr = v
	}
	if v, ok := os.LookupEnv("APP_WORKERS"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("APP_WORKERS 不是整数: %w", err)
		}
		c.Workers = n
	}
	return c, c.Validate()
}

// Validate 启动时就把非法配置挡在门外
func (c Config) Validate() error {
	if c.Workers == 0 {
		return fmt.Errorf("workers 不能为 0")
	}
	return nil
}

func main() {
	c, err := Load()
	if err != nil {
		fmt.Println("配置加载失败:", err)
		return
	}
	fmt.Println("监听:", c.Addr, "协程:", c.Workers, "调试:", c.Debug)
}
