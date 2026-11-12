package main

import (
	"fmt"
	"reflect"
)

type User struct {
	Name string `json:"name" validate:"required"`
	Age  int    `json:"age"`
}

func main() {
	u := User{Name: "Gopher", Age: 8}

	t := reflect.TypeOf(u)
	v := reflect.ValueOf(u)
	fmt.Println(t.Name(), t.Kind()) // User struct
	fmt.Println(v.Kind())           // struct
	fmt.Println(v.Field(0).String()) // Gopher
	fmt.Println(v.Field(1).Int())    // 8

	// 从反射对象回到接口值，再做类型断言。
	fmt.Println(v.Interface().(User).Name) // Gopher
}
