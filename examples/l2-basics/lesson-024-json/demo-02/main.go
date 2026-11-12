package main

import (
	"encoding/json"
	"fmt"
)

type Config struct {
	Retries int     `json:"retries,omitempty"`
	Debug   bool    `json:"debug,omitempty"`
	Ratio   float64 `json:"ratio,omitempty"`
	Name    string  `json:"name,omitempty"`
}

func main() {
	c := Config{Retries: 0, Debug: false, Ratio: 0, Name: ""}
	b, _ := json.Marshal(c)
	fmt.Println(string(b)) // {}：四个零值字段全被省掉

	// 数字解进 any 时一律是 float64，超过 2^53 就丢精度。
	var big int64 = 9007199254740993
	raw := []byte(fmt.Sprintf(`{"id":%d}`, big))
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(m["id"]) // 末位已经变了
}
