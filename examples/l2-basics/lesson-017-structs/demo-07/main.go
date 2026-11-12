package main

import (
	"fmt"
	"unsafe"
)

type A struct {
	Flag  bool
	Count int64
	Tail  bool
}

type User struct {
	Name string
	Tags []string
}

func main() {
	var a A
	fmt.Println("A 总大小 =", unsafe.Sizeof(a))
	fmt.Println("Count 偏移 =", unsafe.Offsetof(a.Count), "Tail 偏移 =", unsafe.Offsetof(a.Tail))
	fmt.Println("int64 对齐 =", unsafe.Alignof(a.Count), "A 对齐 =", unsafe.Alignof(a))

	u1 := User{Name: "a", Tags: []string{"go", "rust"}}
	u2 := u1
	u2.Tags[0] = "zig"
	fmt.Println("浅拷贝后 u1.Tags =", u1.Tags, "u2.Tags =", u2.Tags)

	u4 := User{Name: "a", Tags: []string{"go", "rust"}}
	u3 := u4
	u3.Tags = append([]string(nil), u4.Tags...)
	u3.Tags[0] = "zig"
	fmt.Println("深拷贝后 u4.Tags =", u4.Tags, "u3.Tags =", u3.Tags)

	fmt.Println("空结构体大小 =", unsafe.Sizeof(struct{}{}))
}
