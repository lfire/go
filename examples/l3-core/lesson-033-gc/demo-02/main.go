package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

var sinkValue byte

func workload() {
	var sum byte
	for i := 0; i < 50; i++ {
		buf := make([]byte, 1<<20)
		buf[0] = byte(i)
		sum += buf[0] + buf[len(buf)-1]
	}
	sinkValue = sum
}

func report(tag string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("%s: NumGC=%d, HeapAlloc=%d MiB, 累计暂停=%d ms\n",
		tag, m.NumGC, m.HeapAlloc>>20, m.PauseTotalNs/1e6)
}

func main() {
	workload()
	report("GOGC=100")

	debug.SetGCPercent(20) // 堆增长 20% 就触发：堆更小、GC 更频繁
	workload()
	report("GOGC=20 ")

	debug.SetMemoryLimit(48 << 20) // 软上限 48 MiB，Go 1.19+
	workload()
	report("GOGC=20+Limit")
}
