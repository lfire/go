package main

import (
	"fmt"
	"unsafe"
)

// hmap 是 runtime/map.go 经典结构的镜像，仅用于教学观察
// Go 1.24 起默认换成 Swiss Table，本镜像不适用
type hmap struct {
	count      int
	flags      uint8
	B          uint8
	noverflow  uint16
	hash0      uint32
	buckets    unsafe.Pointer
	oldbuckets unsafe.Pointer
	nevacuate  uintptr
	extra      unsafe.Pointer
}

func fieldB(m map[int]int) uint8 {
	return (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m))).B
}

func main() {
	for _, hint := range []int{0, 8, 9, 13, 14, 52, 53, 10000} {
		b := fieldB(make(map[int]int, hint))
		fmt.Printf("hint %5d → B=%2d 桶数=%d\n", hint, b, 1<<b)
	}
}
