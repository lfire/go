package main

/*
#include <stdlib.h>
#include <string.h>

static int c_strlen(const char *s) {
	return (int)strlen(s);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func main() {
	// C.CString 在 C 堆分配，Go 的 GC 管不到，必须手动释放
	cstr := C.CString("hello from go")
	defer C.free(unsafe.Pointer(cstr))

	n := C.c_strlen(cstr)
	fmt.Printf("C 侧长度 = %d\n", int(n))

	// C.GoString 把 C 字符串复制回 Go 堆，得到受 GC 管理的 string
	fmt.Println("复制回 Go:", C.GoString(cstr))
}
