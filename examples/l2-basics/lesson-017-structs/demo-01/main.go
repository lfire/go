package main

import "fmt"

type User struct {
	ID   int
	Name string
	Age  int
}

func main() {
	var u User // 零值：每个字段各取零值
	fmt.Printf("零值 %+v\n", u)

	u1 := User{ID: 1, Name: "alice", Age: 18} // 键值式
	u2 := User{2, "bob", 20}                  // 位置式，须写全
	fmt.Println(u1.Name, u2.Name)

	u3 := &User{Name: "carol"}
	u3.Age = 21 // 指针自动解引用
	fmt.Printf("%+v\n", u3)
}
