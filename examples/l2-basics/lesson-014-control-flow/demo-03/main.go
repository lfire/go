package main

import "fmt"

func main() {
	// 命中即结束；fallthrough 才继续下一个 case
	switch day := 3; day {
	case 1, 2:
		fmt.Println("工作日")
	case 3:
		fmt.Println("周三")
		fallthrough
	case 4:
		fmt.Println("邻近周四")
	default:
		fmt.Println("其它")
	}

	score := 72
	switch {
	case score >= 90:
		fmt.Println("优")
	case score >= 60:
		fmt.Println("及格")
	default:
		fmt.Println("不及格")
	}

	var x any = 3.14
	switch v := x.(type) {
	case int:
		fmt.Println("整数", v)
	case string:
		fmt.Println("字符串", v)
	default:
		fmt.Println("其它类型", v)
	}
}
