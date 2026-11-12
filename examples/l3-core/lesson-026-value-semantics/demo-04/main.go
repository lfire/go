package main

import (
	"fmt"
	"runtime"
	"strconv"
	"unsafe"
)

type Desc struct {
	ptr unsafe.Pointer
	len int
	cap int
}

func main() {
	fmt.Println("GOARCH:", runtime.GOARCH, " IntSize:", strconv.IntSize)
	fmt.Println("指针:", unsafe.Sizeof(uintptr(0)))
	fmt.Println("string:", unsafe.Sizeof(""))
	fmt.Println("[]int:", unsafe.Sizeof([]int{}))
	fmt.Println("Desc:", unsafe.Sizeof(Desc{}))
}
