package main

import (
	"fmt"
	"strconv"
)

type Client struct{ cache map[string]string }

// Fetch 模拟从远端拉取；err 非空时由 Load 回退本地缓存。
func Fetch(remote map[string]string, err error) (map[string]string, error) {
	if err != nil {
		return nil, err
	}
	return remote, nil
}

func (c *Client) Load(remote map[string]string, fetchErr error) map[string]string {
	got, err := Fetch(remote, fetchErr)
	if err != nil {
		fmt.Println("远端不可用，回退本地缓存")
		return c.cache
	}
	// 校验：非法值不覆盖当前配置，避免一次错误发布打挂全站
	if ms, e := strconv.Atoi(got["timeout_ms"]); e != nil || ms <= 0 {
		fmt.Println("配置校验失败，保留旧值")
		return c.cache
	}
	c.cache = got
	return c.cache
}

// InGray 用稳定的实例 ID 取模做灰度，同一实例每次判定一致。
func InGray(instanceID string, percent int) bool {
	h := 0
	for _, b := range []byte(instanceID) {
		h = h*31 + int(b)
	}
	if h < 0 {
		h = -h
	}
	return h%100 < percent
}

func main() {
	c := &Client{cache: map[string]string{"timeout_ms": "500"}}
	fmt.Println("正常发布:", c.Load(map[string]string{"timeout_ms": "800"}, nil))
	fmt.Println("非法值:", c.Load(map[string]string{"timeout_ms": "abc"}, nil))
	fmt.Println("中心失联:", c.Load(nil, fmt.Errorf("connection refused")))
	fmt.Println("灰度命中:", InGray("order-7", 10), InGray("order-8", 10))
}
