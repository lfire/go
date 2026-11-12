package main

import (
	"encoding/json"
	"fmt"
)

type Config struct {
	Port int    `json:"port"`
	Name string `json:"name"`
}

func load(data []byte, dst *Config) error {
	// 真边界：调用方可能传 nil 指针，或给一段非法 JSON
	if dst == nil {
		return fmt.Errorf("目标指针不能为 nil")
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("解析配置失败: %v", err)
	}
	return nil
}

func main() {
	var c Config
	if err := load([]byte(`{"port":8080,"name":"svc"}`), &c); err == nil {
		fmt.Println("配置 =", c.Port, c.Name)
	}

	if err := load([]byte(`{"port":`), &c); err != nil {
		fmt.Println("非法 JSON 被挡住 =", err != nil)
	}

	if err := load([]byte(`{}`), nil); err != nil {
		fmt.Println("nil 目标被挡住 =", err != nil)
	}
}
