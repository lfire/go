package main

import "fmt"

type point struct{ X, Y int64 }

// 返回局部变量指针：p 逃逸到堆，编译后调用 runtime.newobject
func newPoint(x, y int64) *point {
	return &point{X: x, Y: y}
}

func main() {
	p := newPoint(3, 4)
	fmt.Println(p.X, p.Y)
}
