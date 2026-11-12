package main

import "testing"

var sink int

func step() { sink++ }

// 开放编码：单条固定 defer，编译期展开到返回点
func BenchmarkOpenCoded(b *testing.B) {
	for i := 0; i < b.N; i++ {
		func() {
			defer step()
			step()
		}()
	}
}

// 循环内 defer：每条都要注册，退化到 deferprocStack
func BenchmarkLoopDefer(b *testing.B) {
	for i := 0; i < b.N; i++ {
		func() {
			for j := 0; j < 8; j++ {
				defer step()
			}
			step()
		}()
	}
}
