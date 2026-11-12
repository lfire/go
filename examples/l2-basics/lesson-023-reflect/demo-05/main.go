package main

import (
	"fmt"
	"reflect"
)

type Config struct {
	Port int `json:"port"`
	name string
}

func main() {
	c := Config{Port: 8080, name: "local"}

	v := reflect.ValueOf(c)
	fmt.Println("ValueOf(c).CanSet =", v.CanSet())

	p := reflect.ValueOf(&c).Elem()
	fmt.Println("Elem().CanSet =", p.CanSet())
	fPort := p.FieldByName("Port")
	fName := p.FieldByName("name")
	fmt.Println("导出字段 CanSet =", fPort.CanSet(), "未导出字段 CanSet =", fName.CanSet())
	if fPort.CanSet() {
		fPort.SetInt(9090)
	}
	fmt.Println("改后 Port =", c.Port)

	t := reflect.TypeOf(c)
	fmt.Println("json 标签 =", t.Field(0).Tag.Get("json"), "拼错返回空串 =", t.Field(0).Tag.Get("jso") == "")
}
