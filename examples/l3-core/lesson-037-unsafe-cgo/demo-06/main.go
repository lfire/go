package main

/*
#include <stdint.h>
extern void GoPanic(void);
static void c_call_panic(void) { GoPanic(); }
*/
import "C"

//export GoPanic
func GoPanic() { panic("from C callback") }

func main() { C.c_call_panic() }
