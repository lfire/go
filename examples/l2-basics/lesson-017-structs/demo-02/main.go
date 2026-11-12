package main

import "fmt"

type Point struct {
	X, Y int
}

type Circle struct {
	Point  // 嵌入：字段名就是类型名
	Radius int
}

func main() {
	c := Circle{Point: Point{X: 1, Y: 2}, Radius: 5}
	fmt.Println("X =", c.X, "等价", c.Point.X) // 提升字段
	c.X = 9
	fmt.Printf("%+v\n", c)

	// 命名字面量必须逐层写，不能用提升字段直接初始化
	c2 := Circle{Point: Point{3, 4}, Radius: 1}
	fmt.Println(c2.Y)
}
