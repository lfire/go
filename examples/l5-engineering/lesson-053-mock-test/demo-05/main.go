package main

import (
	"fmt"
	"unsafe"
)

type Getter interface{ Get() string }

type mem struct{ v string }

func (m mem) Get() string { return m.v }

func main() {
	var g Getter = mem{v: "ada"}
	x := (*[2]unsafe.Pointer)(unsafe.Pointer(&g))
	fmt.Printf("tab=%p data=%p\n", x[0], x[1])

	// 同一个具体值塞进另一个不同接口：data 指向同一份数据，tab 变了
	var g2 interface{ Get() string } = mem{v: "ada"}
	y := (*[2]unsafe.Pointer)(unsafe.Pointer(&g2))
	fmt.Printf("另一个接口的 tab=%p\n", y[0])
	fmt.Println("两个接口 tab 相同吗:", x[0] == y[0])
}
