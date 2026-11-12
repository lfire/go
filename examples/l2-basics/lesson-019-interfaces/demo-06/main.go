package main

import (
	"errors"
	"fmt"
)

// stringify 只接受能转成字符串的值，遇到未知类型返回错误而不是 panic。
func stringify(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", errors.New("空值")
	case string:
		return x, nil
	case int:
		return fmt.Sprintf("%d", x), nil
	case fmt.Stringer: // 接口也能作为 type switch 的 case
		return x.String(), nil
	default:
		return "", fmt.Errorf("不支持的类型 %T", v)
	}
}

type Code int

func (c Code) String() string { return fmt.Sprintf("CODE-%d", int(c)) }

func main() {
	for _, v := range []any{"go", 42, Code(7), nil, 3.14} {
		if s, err := stringify(v); err == nil {
			fmt.Println("结果 =", s)
		} else {
			fmt.Println("被挡住 =", err != nil)
		}
	}
}
