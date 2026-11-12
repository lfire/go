// 良好命名与注释的示范：导出标识符一律带文档注释
package main

import (
	"fmt"
	"strings"
)

// User 表示系统中的一个用户。导出类型必须紧邻说明性注释。
type User struct {
	ID   int64
	Name string
}

// DisplayName 返回用于展示的名称，去掉首尾空白并加前缀。
// 接收者用短名 u，不用 this 或 self。
func (u User) DisplayName() string {
	return "用户:" + strings.TrimSpace(u.Name)
}

func main() {
	u := User{ID: 7, Name: "  ada  "}
	fmt.Println(u.DisplayName())
}
