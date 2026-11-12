// 从配置文件读取并校验：加载阶段就把问题暴露出来
package main

import (
	"encoding/json"
	"fmt"
)

// Config 用 json tag 声明文件字段
type Config struct {
	Addr    string `json:"addr"`
	Timeout int    `json:"timeout_ms"`
	Mode    string `json:"mode"`
}

func Parse(raw string) (Config, error) {
	var c Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Config{}, fmt.Errorf("解析配置: %w", err)
	}
	if c.Addr == "" {
		return Config{}, fmt.Errorf("addr 不能为空")
	}
	if c.Timeout == 0 {
		return Config{}, fmt.Errorf("timeout_ms 不能为 0")
	}
	switch c.Mode {
	case "dev", "staging", "prod":
	default:
		return Config{}, fmt.Errorf("mode 只能是 dev/staging/prod，当前 %s", c.Mode)
	}
	return c, nil
}

func main() {
	for _, raw := range []string{
		`{"addr":":8080","timeout_ms":500,"mode":"prod"}`,
		`{"addr":":8080","timeout_ms":0,"mode":"prod"}`,
	} {
		c, err := Parse(raw)
		if err != nil {
			fmt.Println("失败:", err)
			continue
		}
		fmt.Println("成功:", c.Addr, c.Mode, c.Timeout)
	}
}
