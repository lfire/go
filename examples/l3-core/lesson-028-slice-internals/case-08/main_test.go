package main

import "testing"

var sink []int

// 不预估容量：append 反复触发 growslice，产生多次分配与旧元素拷贝
func BenchmarkNoCap(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := make([]int, 0)
		for j := 0; j < 1024; j++ {
			s = append(s, j)
		}
		sink = s
	}
}

// 预估容量：一次 make 到位，循环内只写不扩容
func BenchmarkWithCap(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := make([]int, 0, 1024)
		for j := 0; j < 1024; j++ {
			s = append(s, j)
		}
		sink = s
	}
}
