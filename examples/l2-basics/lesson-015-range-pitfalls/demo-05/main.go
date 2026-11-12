package main

import "fmt"

type User struct {
	Name  string
	Score int
}

// validate 检查指针切片里有没有 nil 元素或负分，发现即整体返回错误。
func validate(users []*User) error {
	for i, u := range users {
		if u == nil { // 真边界：指针切片可能混入 nil
			return fmt.Errorf("第 %d 个元素是 nil", i)
		}
		if u.Score < 0 {
			return fmt.Errorf("用户 %s 分数非法", u.Name)
		}
	}
	return nil
}

func main() {
	users := []*User{{Name: "a", Score: 90}, {Name: "b", Score: 80}}
	fmt.Println("全部合法 =", validate(users) == nil)

	users = append(users, nil) // 混入一个 nil 指针
	fmt.Println("含 nil 时报错 =", validate(users) != nil)

	fmt.Println("nil 切片也合法 =", validate(nil) == nil)
}
