package main

import (
	"fmt"
	"math"
)

func main() {
	x := 0.1              // 先把两个数存成 float64 变量
	y := 0.2
	a := x + y            // 运行时相加，误差才会显现
	fmt.Println("0.1 加 0.2：", a)
	fmt.Println("与 0.3 相等吗：", a == 0.3)
	fmt.Println("用误差判断：", math.Abs(a-0.3) < 1e-9)
	fmt.Println("保留两位：", math.Round(a*100)/100)
}
