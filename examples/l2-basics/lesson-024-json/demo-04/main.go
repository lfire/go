package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Meta struct{ Page int }

type Req struct {
	Meta    Meta  `json:"meta,omitempty"`
	MetaPtr *Meta `json:"meta_ptr,omitempty"`
	Retries int   `json:"retries,omitempty"`
}

func main() {
	b, _ := json.Marshal(Req{})
	fmt.Println("omitempty 对结构体仍输出 meta =", strings.Contains(string(b), "meta"))
	fmt.Println("指针字段被省略 =", !strings.Contains(string(b), "meta_ptr"))

	var u struct{ Name string }
	err := json.Unmarshal([]byte(`{"Name":"go"}`), u)
	fmt.Println("传值的错误 =", err != nil)

	dec := json.NewDecoder(strings.NewReader(`{"id":9007199254740993}`))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		fmt.Println("解码失败 =", err)
		return
	}
	n, _ := m["id"].(json.Number).Int64()
	fmt.Println("UseNumber 后 =", n)
}
