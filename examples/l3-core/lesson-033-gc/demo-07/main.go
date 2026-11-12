package main

import (
	"fmt"
	"runtime"
	"unsafe"
)

type PtrNode struct { // 含指针：GC 必须递归扫描
	Next *PtrNode
	Val  int64
}
type ValNode struct { // 无指针：分配器打 noscan，GC 完全跳过
	Idx int32
	Val int64
}

var keepP []*PtrNode
var keepV []ValNode

func main() {
	const N = 200000
	keepP = make([]*PtrNode, N)
	keepV = make([]ValNode, N)
	for i := range keepP {
		keepP[i] = &PtrNode{Val: int64(i)}
		keepV[i] = ValNode{Idx: int32(i), Val: int64(i)}
	}
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("PtrNode=%d B, ValNode=%d B, HeapObjects=%d, 目标=%d MiB\n",
		unsafe.Sizeof(PtrNode{}), unsafe.Sizeof(ValNode{}), m.HeapObjects, m.HeapAlloc>>20)
}
