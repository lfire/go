package main

import "fmt"

func describe(x any) {
	// comma-ok 断言：失败也不 panic
	if s, ok := x.(string); ok {
		fmt.Println("字符串长度 =", len(s))
	}

	// type switch：按动态类型分支
	switch v := x.(type) {
	case int:
		fmt.Println("整数 =", v)
	case string:
		fmt.Println("字符串 =", v)
	case nil:
		fmt.Println("nil")
	default:
		fmt.Println("其它类型")
	}
}

func main() {
	describe(42)
	describe("go")
	describe(nil)

	var a any = 7
	n := a.(int) // 确定类型才用单值形式
	fmt.Println("断言得到", n)
}
