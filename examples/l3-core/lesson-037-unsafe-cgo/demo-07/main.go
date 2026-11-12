package main

/*
#include <stdlib.h>
static void **c_slot(void) {
    static void *p;
    return &p;
}
*/
import "C"

import "unsafe"

func main() {
	x := 42
	gp := unsafe.Pointer(&x)
	slot := C.c_slot()
	*(*unsafe.Pointer)(unsafe.Pointer(slot)) = gp // 把 Go 指针写进 C 静态内存
	_ = x
}
