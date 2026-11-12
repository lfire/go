package main

import (
	"encoding/json"
	"fmt"
)

// UserV1 模拟 proto 里的消息：json tag 里的数字类比 protobuf 的字段号。
// protobuf 靠字段号而非字段名定位数据，这是跨版本兼容的根基。
type UserV1 struct {
	ID   uint64 `json:"1"`
	Name string `json:"2"`
}

// UserV2 新增了 Email（字段号 3），老字段号 1、2 一动不动。
type UserV2 struct {
	ID    uint64 `json:"1"`
	Name  string `json:"2"`
	Email string `json:"3"`
}

func main() {
	// 老程序发出的报文，只有字段 1、2
	oldWire := `{"1":1001,"2":"alice"}`

	// 新程序解码：新增字段 3 缺失，自动取零值——这就是默认值语义
	var v2 UserV2
	_ = json.Unmarshal([]byte(oldWire), &v2)
	fmt.Printf("新程序读老报文: ID=%d Name=%s Email=%q（零值）\n", v2.ID, v2.Name, v2.Email)

	// 新程序发出的报文，老程序解码：未知字段 3 被忽略，不报错
	newWire := `{"1":1001,"2":"alice","3":"a@example.com"}`
	var v1 UserV1
	_ = json.Unmarshal([]byte(newWire), &v1)
	fmt.Printf("老程序读新报文: ID=%d Name=%s（未知字段被忽略）\n", v1.ID, v1.Name)
}
