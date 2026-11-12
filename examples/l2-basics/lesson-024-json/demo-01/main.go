package main

import (
	"encoding/json"
	"fmt"
)

// User 只有可导出字段才会参与编解码。
type User struct {
	Name  string `json:"name"`
	Age   int    `json:"age"`
	Token string `json:"-"` // 完全不参与
	Role  string `json:"role,omitempty"`
	token string // 小写字段：反射不可见，被跳过
}

func main() {
	u := User{Name: "Gopher", Age: 8, Token: "secret", token: "hidden"}
	b, err := json.Marshal(u)
	if err != nil {
		fmt.Println("编码失败:", err)
		return
	}
	fmt.Println(string(b))

	var back User
	if err := json.Unmarshal(b, &back); err != nil {
		fmt.Println("解码失败:", err)
		return
	}
	fmt.Println(back.Name, back.Age)
}
