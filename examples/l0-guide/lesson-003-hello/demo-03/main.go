package main

import (
	"fmt"
	"math"
)

func main() {
	// math.Pi 首字母大写，说明它是可供外部使用的导出常量
	fmt.Printf("圆周率约等于 %.2f\n", math.Pi)
}
