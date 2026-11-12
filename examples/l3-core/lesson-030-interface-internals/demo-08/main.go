package main

import (
	"fmt"
	"reflect"
)

func inspect(v any) {
	if s, ok := v.(string); ok { // comma-ok 断言，失败不 panic
		fmt.Println("字符串长度:", len(s))
	}
	switch x := v.(type) { // type switch 读的是头里的动态类型
	case int:
		fmt.Println("int 加一:", x+1)
	default:
		fmt.Println("其它类型:", reflect.TypeOf(v))
	}
	val := reflect.ValueOf(v)
	fmt.Println("Kind:", val.Kind()) // 反射同样依赖接口保存的类型信息
}

func main() {
	inspect(42)
	inspect("go")
}
