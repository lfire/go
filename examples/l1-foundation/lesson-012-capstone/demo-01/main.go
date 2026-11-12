package main

import (
	"flag"
	"fmt"
)

func main() {
	from := flag.String("from", "km", "源单位")     // 字符串参数，默认值 km
	to := flag.String("to", "m", "目标单位")        // 默认值 m
	value := flag.Float64("value", 1, "要换算的数值") // 浮点参数，默认值 1
	flag.Parse() // 必须在读取参数之前调用
	fmt.Println("源：", *from, "目标：", *to, "数值：", *value)
}
