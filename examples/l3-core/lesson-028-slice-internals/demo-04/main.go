package main

import (
	"fmt"
	"runtime"
	"strconv"
	"unsafe"
)

type hdr struct {
	Data unsafe.Pointer
	Len  int
	Cap  int
}

func main() {
	var h hdr
	fmt.Println("GOARCH:", runtime.GOARCH, " IntSize:", strconv.IntSize)
	fmt.Println("头大小:", unsafe.Sizeof(h))
	fmt.Println("Data/Len/Cap 偏移:",
		unsafe.Offsetof(h.Data), unsafe.Offsetof(h.Len), unsafe.Offsetof(h.Cap))
	var nilS []int
	emptyS := make([]int, 0)
	fmt.Printf("nil Data=%v len=%d cap=%d\n",
		(*hdr)(unsafe.Pointer(&nilS)).Data, len(nilS), cap(nilS))
	fmt.Printf("empty Data=%v len=%d cap=%d\n",
		(*hdr)(unsafe.Pointer(&emptyS)).Data, len(emptyS), cap(emptyS))
}
