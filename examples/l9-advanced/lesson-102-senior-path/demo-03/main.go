package main

import "fmt"

type Entry struct {
	Kind  string
	Title string
}

// 输出台账：把源码笔记、分享、PR、复盘都记下来，形成可回顾的复利。
func main() {
	log := []Entry{
		{"源码笔记", "runtime/proc.go 调度循环"},
		{"分享", "GMP 与异步抢占"},
		{"PR", "修复一处数据竞争"},
		{"复盘", "一次 p99 抖动"},
		{"分享", "GOGC 调优闭环"},
	}
	counts := map[string]int{}
	for _, e := range log {
		counts[e.Kind]++
	}
	for _, k := range []string{"源码笔记", "分享", "PR", "复盘"} {
		fmt.Println(k, counts[k])
	}
	fmt.Println("累计输出条数 =", len(log))
}
