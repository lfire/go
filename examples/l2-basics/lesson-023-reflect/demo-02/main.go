package main

import (
	"fmt"
	"reflect"
)

func main() {
	x := 10
	v := reflect.ValueOf(x)
	fmt.Println(v.CanSet()) // false：传的是值的副本

	p := reflect.ValueOf(&x).Elem() // 顺着指针找到正本
	fmt.Println(p.CanSet())         // true：可寻址且可写
	p.SetInt(42)
	fmt.Println(x) // 42

	// 未导出字段即使可寻址，也不允许写。
	type cfg struct {
		Port int
		name string
	}
	c := cfg{Port: 8080, name: "local"}
	cp := reflect.ValueOf(&c).Elem()
	fmt.Println(cp.Field(0).CanSet()) // true
	fmt.Println(cp.Field(1).CanSet()) // false：小写字段不可导出
}
