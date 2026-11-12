package main

import "fmt"

func main() {
	age := map[string]int{"alice": 18, "bob": 20}
	score := make(map[string]int, 8)

	age["carol"] = 22 // 增改都是赋值
	score["alice"] = 90

	fmt.Println("age[bob] =", age["bob"])
	fmt.Println("age[nobody] =", age["nobody"]) // 缺失时给零值

	if v, ok := age["bob"]; ok {
		fmt.Println("找到", v)
	}
	if _, ok := age["nobody"]; !ok {
		fmt.Println("键不存在")
	}

	delete(age, "alice") // 删不存在的键也安全
	delete(age, "alice")
	fmt.Println("删除后 len =", len(age), "score len =", len(score))
}
