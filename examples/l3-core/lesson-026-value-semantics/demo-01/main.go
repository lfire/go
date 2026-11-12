package main

import "fmt"

// Point 的字段全是值，赋值时会整份复制
type Point struct {
	X, Y int
	Tag  string
}

func main() {
	a := Point{X: 1, Y: 2, Tag: "origin"}
	b := a // 结构体赋值：整份拷贝
	b.X = 100
	fmt.Println("a =", a, " b =", b) // a 仍是原值，b 是独立副本

	arr1 := [3]int{1, 2, 3}
	arr2 := arr1 // 数组是值：3 个 int 全部拷贝
	arr2[0] = 99
	fmt.Println("arr1 =", arr1, " arr2 =", arr2)

	s1 := []int{1, 2, 3}
	s2 := s1 // 切片是描述符：只拷贝头，底层数据共享
	s2[0] = 99
	fmt.Println("s1 =", s1, " s2 =", s2) // s1[0] 也变成 99
}
