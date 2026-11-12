package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"time"
)

// 观察二：同一份分配压力在 GOGC=100 与 GOGC=400 下的 GC 次数与耗时。
var sink [][]byte

func workload() {
	for i := 0; i < 120; i++ {
		sink = append(sink, make([]byte, 1<<20))
		if len(sink) > 40 {
			sink = sink[len(sink)-20:]
		}
	}
}

func run(percent int) (int, time.Duration) {
	debug.SetGCPercent(percent)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	workload()
	elapsed := time.Since(start)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	return int(after.NumGC - before.NumGC), elapsed
}

func main() {
	for _, p := range []int{100, 400} {
		n, d := run(p)
		fmt.Printf("GOGC=%d 触发 %d 次 GC，耗时 %v\n", p, n, d)
	}
}
