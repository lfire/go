package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

func unitFactor(unit string) (float64, error) {
	if unit == "km" {
		return 1000, nil
	}
	if unit == "m" {
		return 1, nil
	}
	if unit == "cm" {
		return 0.01, nil
	}
	return 0, errors.New("未知单位，只支持 km / m / cm")
}

// convert 把 value 从 from 单位换算到 to 单位
func convert(value float64, from string, to string) (float64, error) {
	if value < 0 {
		return 0, errors.New("长度不能为负")
	}
	f, err := unitFactor(from)
	if err != nil {
		return 0, err
	}
	t, err := unitFactor(to)
	if err != nil {
		return 0, err
	}
	return value * f / t, nil
}

func main() {
	from := flag.String("from", "km", "源单位：km / m / cm")
	to := flag.String("to", "m", "目标单位：km / m / cm")
	value := flag.Float64("value", 1, "要换算的数值")
	flag.Parse()

	result, err := convert(*value, *from, *to)
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err) // 错误走标准错误
		os.Exit(1)                          // 非零退出码告诉调用方失败
	}
	fmt.Printf("%.2f %s = %.4f %s\n", *value, *from, result, *to)
}
