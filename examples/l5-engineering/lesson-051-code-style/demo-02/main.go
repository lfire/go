// 错误必须被检查并带上下文：errcheck 与 %w 的规范化写法
package main

import (
	"errors"
	"fmt"
)

var errNotFound = errors.New("记录不存在")

// load 模拟一次查询，返回的错误必须被上层处理。
func load(id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("load: 缺少 id: %w", errNotFound)
	}
	return "data-" + id, nil
}

func main() {
	v, err := load("")
	if err != nil {
		// 用 errors.Is 判定，而不是拿字符串去比
		fmt.Println("是否未找到:", errors.Is(err, errNotFound))
		fmt.Println("带上下文的错误:", err)
		return
	}
	fmt.Println(v)
}
