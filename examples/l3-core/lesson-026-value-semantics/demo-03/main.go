package main

import (
	"fmt"
	"unsafe"
)

func main() {
	var i int
	var s string
	var sl []int
	var m map[string]int
	var e any
	var st struct{ A, B, C int }
	fmt.Println("int        ", unsafe.Sizeof(i))                // 8
	fmt.Println("string     ", unsafe.Sizeof(s))                // 16：Data 指针 + Len
	fmt.Println("[]int      ", unsafe.Sizeof(sl))               // 24：Data + Len + Cap
	fmt.Println("map        ", unsafe.Sizeof(m))                // 8：一个 *hmap 指针
	fmt.Println("interface  ", unsafe.Sizeof(e))                // 16：类型字 + 数据字
	fmt.Println("struct 3int", unsafe.Sizeof(st))               // 24：字段之和
	fmt.Println("[1024]int  ", unsafe.Sizeof([1024]int{}))      // 8192：真·大对象
}
