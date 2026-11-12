package main

import (
	"fmt"
	"runtime/metrics"
)

func main() {
	// 先制造不同尺寸的对象，让直方图非空
	var sink [][]byte
	for _, n := range []int{8, 24, 100, 4096, 1 << 16} {
		sink = append(sink, make([]byte, n))
	}

	// 该指标按 size class 分桶统计累计分配字节对应的次数
	const name = "/gc/heap/allocs-by-size:bytes"
	sample := []metrics.Sample{{Name: name}}
	metrics.Read(sample)
	h := sample[0].Value.Float64Histogram()
	for i, c := range h.Counts {
		if c > 0 {
			fmt.Printf("[%6.0f, %6.0f) B: %d 次\n",
				h.Buckets[i], h.Buckets[i+1], c)
		}
	}
	_ = sink
}
