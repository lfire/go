package main

import (
	"fmt"
	"testing"
)

type Box struct{ P *int }

//go:noinline
func leakToResult(p *int) *Box { return &Box{P: p} } // 参数原样进返回值

//go:noinline
func readOnly(p *int) int { return *p } // 只读，不外泄

func newIntA() *int { x := 1; return &x } // 允许内联

//go:noinline
func newIntB() *int { x := 2; return &x } // 强制保留独立函数

func callerA() int { return *newIntA() }
func callerB() int { return *newIntB() }

var sink int

func main() {
	x := 1
	_ = leakToResult(&x)
	_ = readOnly(&x)
	a := testing.AllocsPerRun(1000, func() { sink = callerA() })
	b := testing.AllocsPerRun(1000, func() { sink = callerB() })
	fmt.Printf("callerA(newIntA 内联): %.0f allocs/op\n", a)
	fmt.Printf("callerB(newIntB 独立): %.0f allocs/op\n", b)
}
