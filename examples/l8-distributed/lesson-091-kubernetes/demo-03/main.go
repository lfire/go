package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// 优先读挂载的 Secret 文件，其次读环境变量，最后用默认值
func readSecret(name, envKey, mountDir, fallback string) string {
	p := filepath.Join(mountDir, name)
	if b, err := os.ReadFile(p); err == nil {
		return string(b)
	}
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return fallback
}

func main() {
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	_ = readSecret("db-password", "DB_PASSWORD", "/etc/secret", "not-set")
	fmt.Println("日志级别:", logLevel)
	fmt.Println("口令读取顺序: 挂载文件 -> 环境变量 -> 默认值")
	fmt.Println("Secret 不以明文打印，避免泄露到日志")
}
