package main

import "fmt"

type User struct{ Age int }

func reset(m map[string]int) {
	m = make(map[string]int)
}

func clearAll(m map[string]int) {
	for k := range m {
		delete(m, k)
	}
}

func main() {
	users := make(map[string]User, 16)
	users["a"] = User{Age: 18}
	u := users["a"]
	u.Age = 20
	users["a"] = u
	fmt.Println("取出改回写 =", users["a"].Age)

	ptrs := map[string]*User{"a": &User{Age: 18}}
	ptrs["a"].Age = 30
	fmt.Println("指针 value 直接改 =", ptrs["a"].Age)

	m := map[string]int{"x": 1}
	reset(m)
	fmt.Println("重新赋值后调用方 =", m)
	clearAll(m)
	fmt.Println("删键后调用方 =", m)

	byArray := map[[3]int]string{}
	byArray[[3]int{1, 2, 3}] = "ok"
	fmt.Println("数组键 =", byArray[[3]int{1, 2, 3}])

	var nilMap map[string]int
	clear(nilMap)
	fmt.Println("nil map 清空后 len =", len(nilMap))
}
