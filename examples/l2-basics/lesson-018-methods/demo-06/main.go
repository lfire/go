package main

import "fmt"

type Speaker interface{ Speak() string }

type Dog struct{}

func (Dog) Speak() string { return "wang" }

type Cat struct{}

func (c *Cat) Speak() string { return "miao" }

type Team struct {
	Name    string
	Members []string
}

// 值接收者：改引用型字段会回写到共享底层数组
func (t Team) RenameFirst(v string) {
	t.Members[0] = v
}

func main() {
	var s Speaker
	s = Dog{}
	fmt.Println("值类型满足 =", s.Speak())
	s = &Cat{}
	fmt.Println("指针满足 =", s.Speak())

	team := Team{Name: "a", Members: []string{"go", "rust"}}
	team.RenameFirst("zig")
	fmt.Println("值接收者改切片字段后 =", team.Members)

	var _ Speaker = (*Cat)(nil)
}
