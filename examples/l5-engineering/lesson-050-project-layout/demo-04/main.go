package main

import (
	"encoding/json"
	"fmt"
)

// UserDO 是存储对象，密码哈希与内部状态码只应存在于数据层内部。
type UserDO struct {
	ID           int64  `json:"id"`
	PasswordHash string `json:"password_hash"`
	Status       int    `json:"status"`
}

// UserVO 是展示对象，只暴露前端确实需要的字段。
type UserVO struct {
	Name string `json:"name"`
}

// toVO 是数据离开业务层的唯一出口。
func toVO(u UserDO) UserVO {
	return UserVO{Name: fmt.Sprintf("用户%d", u.ID)}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "序列化失败: " + err.Error()
	}
	return string(b)
}

func main() {
	u := UserDO{ID: 1001, PasswordHash: "x9f3c2", Status: 2}
	fmt.Println("直接返回 DO:", mustJSON(u))
	fmt.Println("返回 VO:    ", mustJSON(toVO(u)))
}
