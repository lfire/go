package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DataFile string `json:"data_file"`
	LockFile string `json:"lock_file"`
	Priority int    `json:"default_priority"`
	TTLHours int    `json:"ttl_hours"`
}

// defaults 是优先级最低的一层。
func defaults() Config {
	return Config{Priority: 1, TTLHours: 720}
}

// loadFile 叠加第二层：配置文件；文件不存在视为合法。
func loadFile(c Config, path string) (Config, error) {
	if path == "" {
		return c, nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("读配置: %w", err)
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("解析配置: %w", err)
	}
	return c, nil
}

// applyEnv 叠加第三层：环境变量，只覆盖显式设置的键。
func applyEnv(c Config) (Config, error) {
	if v := os.Getenv("TDL_DATA_FILE"); v != "" {
		c.DataFile = v
	}
	if v := os.Getenv("TDL_PRIORITY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return c, fmt.Errorf("TDL_PRIORITY 非法: %w", err)
		}
		c.Priority = n
	}
	return c, nil
}

// applyFlag 叠加最高层：命令行 flag，空串与负数表示未设置。
func applyFlag(c Config, dataFile string, priority int) Config {
	if dataFile != "" {
		c.DataFile = dataFile
	}
	if priority >= 0 {
		c.Priority = priority
	}
	return c
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return home + p[1:]
	}
	return p
}

func main() {
	c, err := loadFile(defaults(), "")
	if err == nil {
		c, err = applyEnv(c)
	}
	if err == nil {
		c = applyFlag(c, "~/.todo/todo.json", -1)
		c.DataFile = expandHome(c.DataFile)
		fmt.Printf("data=%s priority=%d ttl=%dh\n", c.DataFile, c.Priority, c.TTLHours)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
