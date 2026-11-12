package main

import (
	"fmt"
	"strconv"
)

// Kind 是工具支持的最小类型系统。
type Kind int

const (
	Null Kind = iota
	Int
	Float
	Str
	Bool
)

// Value 用 Valid 区分 NULL 与零值：空串是 NULL，不是 0 也不是空字符串值。
type Value struct {
	kind  Kind
	num   float64
	str   string
	valid bool
}

func NullValue() Value           { return Value{kind: Null} }
func IntValue(n int64) Value     { return Value{kind: Int, num: float64(n), valid: true} }
func FloatValue(f float64) Value { return Value{kind: Float, num: f, valid: true} }
func StrValue(s string) Value    { return Value{kind: Str, str: s, valid: true} }

// Parse 把 CSV 原始单元格按声明类型转成 Value；空串或转型失败一律判 NULL。
func Parse(cell string, k Kind) Value {
	if cell == "" {
		return NullValue()
	}
	switch k {
	case Int:
		if n, err := strconv.ParseInt(cell, 10, 64); err == nil {
			return IntValue(n)
		}
	case Float:
		if f, err := strconv.ParseFloat(cell, 64); err == nil {
			return FloatValue(f)
		}
	case Str:
		return StrValue(cell)
	case Bool:
		if b, err := strconv.ParseBool(cell); err == nil {
			return StrValue(strconv.FormatBool(b))
		}
	}
	return NullValue()
}

func (v Value) IsNull() bool { return !v.valid }

func main() {
	for _, c := range []string{"200", "", "abc"} {
		v := Parse(c, Int)
		fmt.Printf("cell=%q null=%v num=%.0f\n", c, v.IsNull(), v.num)
	}
}
