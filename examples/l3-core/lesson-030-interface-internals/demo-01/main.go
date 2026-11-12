package main

import (
	"fmt"
	"unsafe"
)

// eface / iface 是 runtime 里两个字的镜像，仅用于教学观察
type eface struct {
	typ  unsafe.Pointer // 动态类型 *_type
	data unsafe.Pointer // 数据指针
}
type iface struct {
	tab  unsafe.Pointer // *itab：接口类型 + 动态类型 + 方法表
	data unsafe.Pointer
}

func main() {
	var a any
	var r interface {
		Read([]byte) (int, error)
	}
	fmt.Println("any 大小:", unsafe.Sizeof(a))    // 16
	fmt.Println("非空接口大小:", unsafe.Sizeof(r)) // 16
	fmt.Println("eface / iface 镜像:", unsafe.Sizeof(eface{}), unsafe.Sizeof(iface{}))
}
