package main

import "fmt"

func main() {
	// n 只在 if/else 链内可见
	if n := 7; n%2 == 0 {
		fmt.Println("偶数")
	} else {
		fmt.Println("奇数", n)
	}
	// fmt.Println(n) // 放到这里会编译失败

	score := 85
	if score >= 60 {
		fmt.Println("及格")
	}
}
