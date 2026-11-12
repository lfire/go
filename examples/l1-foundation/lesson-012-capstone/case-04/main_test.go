package main

import (
	"errors"
	"testing"
)

// 真实项目里 unitFactor 在 convert.go；这里放进同一段，方便整段直接运行
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
	return 0, errors.New("未知单位")
}

func TestUnitFactor(t *testing.T) {
	cases := []struct {
		name string
		unit string
		want float64
	}{
		{"公里", "km", 1000},
		{"米", "m", 1},
		{"厘米", "cm", 0.01},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := unitFactor(tc.unit)
			if err != nil {
				t.Fatalf("意外报错：%v", err)
			}
			if got != tc.want {
				t.Errorf("得到 %v，想要 %v", got, tc.want)
			}
		})
	}
}
