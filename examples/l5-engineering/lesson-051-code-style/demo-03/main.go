// gofmt 与 goimports 只管排版与 import，不改变语义
package main

import (
	"fmt"
	"time"
)

// 常量分组与 iota 是 Effective Go 推荐的写法
const (
	LevelDebug int = iota
	LevelInfo
	LevelWarn
)

func main() {
	t := time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC)
	// 动词与参数匹配，go vet 会检查 Printf 家族的调用
	fmt.Printf("级别=%d 时间=%s\n", LevelWarn, t.Format("15:04"))
}
