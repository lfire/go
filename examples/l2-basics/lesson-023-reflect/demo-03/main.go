package main

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

type User struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

// FillByTag 按 json 标签定位字段并写入，模拟解码器的一小步。
func FillByTag(dst any, key, val string) {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		panic("需要非空指针")
	}
	e := v.Elem()
	t := e.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := strings.Split(field.Tag.Get("json"), ",")[0]
		if tag != key {
			continue
		}
		f := e.Field(i)
		if !f.CanSet() {
			continue
		}
		switch f.Kind() {
		case reflect.String:
			f.SetString(val)
		case reflect.Int:
			n, _ := strconv.Atoi(val)
			f.SetInt(int64(n))
		}
	}
}

func main() {
	var u User
	FillByTag(&u, "name", "Gopher")
	FillByTag(&u, "age", "18")
	fmt.Println(u.Name, u.Age) // Gopher 18
}
