package main

import "fmt"

type Handler interface{ Handle(string) string }

type Legacy struct{}

func (Legacy) Handle(s string) string { return "legacy:" + s }

type Modern struct{}

func (Modern) Handle(s string) string { return "modern:" + s }

// 绞杀者门面：按 key 的哈希灰度，把一部分流量导向新实现。
type Facade struct {
	legacy    Handler
	modern    Handler
	modernPct int
}

func (f Facade) Handle(key string) string {
	if hash(key)%100 < f.modernPct {
		return f.modern.Handle(key)
	}
	return f.legacy.Handle(key)
}

func hash(s string) int {
	h := 0
	for i := range s {
		h = h*31 + int(s[i])
	}
	return h
}

func main() {
	f := Facade{legacy: Legacy{}, modern: Modern{}, modernPct: 25}
	for _, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		fmt.Println(k, f.Handle(k))
	}
}
