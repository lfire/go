package main

import "fmt"

type Dim struct {
	Name  string
	Score int
}

// 四维自评：找出最弱一维，把学习计划集中投在它上面。
func main() {
	dims := []Dim{
		{"语言与源码", 4},
		{"工程质量", 3},
		{"系统与架构", 3},
		{"影响力与协作", 2},
	}
	total := 0
	weakest := dims[0]
	for _, d := range dims {
		total += d.Score
		if d.Score < weakest.Score {
			weakest = d
		}
		fmt.Printf("%s = %d 分\n", d.Name, d.Score)
	}
	fmt.Println("平均分 =", float64(total)/float64(len(dims)))
	fmt.Println("最弱维度 =", weakest.Name)
}
