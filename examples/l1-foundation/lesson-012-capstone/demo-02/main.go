package main

import (
	"errors"
	"fmt"
)

// unitFactor 把单位名换算成相对米的倍率，不认识的单位返回错误
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

func main() {
	f, err := unitFactor("km")
	if err != nil {
		fmt.Println("失败：", err)
		return
	}
	fmt.Println("km 的倍率：", f)

	_, err = unitFactor("inch") // 故意给一个不认识的单位
	if err != nil {
		fmt.Println("如预期报错：", err)
	}
}
