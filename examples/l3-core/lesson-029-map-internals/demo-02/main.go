package main

import (
	"fmt"
	"unsafe"
)

// hmap 是 runtime/map.go 经典结构的镜像，仅用于教学观察。
// Go 1.24 起默认换成 Swiss Table，内部布局不同，本镜像不适用。
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

func info(m map[int]int) *hmap {
	return (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
}

func main() {
	m := make(map[int]int)
	fmt.Println("空 map 的 B:", info(m).B)
	for i := 0; i < 14; i++ {
		m[i] = i
	}
	h := info(m)
	fmt.Println("插入 14 个后 count:", h.count)
	fmt.Println("B:", h.B, " 桶数:", uintptr(1)<<h.B)
	fmt.Printf("负载因子: %.2f\n", float64(h.count)/float64(uintptr(1)<<h.B))
}
