package main

import (
	"fmt"
	"os/user"
	"runtime/debug"
)

func main() {
	// os/user 在启用 cgo 时会调系统库；纯 Go 实现改读 /etc/passwd
	u, err := user.Current()
	if err != nil {
		fmt.Println("解析当前用户失败:", err)
	} else {
		fmt.Println("当前用户:", u.Username)
	}
	// 读取嵌进二进制的构建信息，确认 CGO 开关
	if info, ok := debug.ReadBuildInfo(); ok {
		fmt.Println("主模块:", info.Main.Path)
		for _, s := range info.Settings {
			if s.Key == "CGO_ENABLED" {
				fmt.Println("CGO_ENABLED =", s.Value)
			}
		}
	}
}
