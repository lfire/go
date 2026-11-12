package main

import (
	"fmt"
	"runtime"
)

var sinkValue byte

func workload() {
	var sum byte
	for i := 0; i < 20; i++ {
		buf := make([]byte, 4<<20) // 每轮约 4 MiB 的短命对象
		buf[0] = byte(i)
		sum += buf[0] + buf[len(buf)-1]
	}
	sinkValue = sum // 只保留标量，20 轮大对象很快全成垃圾
}

func main() {
	workload()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("NumGC=%d, HeapAlloc=%d MiB, 累计暂停=%d ms\n",
		m.NumGC, m.HeapAlloc>>20, m.PauseTotalNs/1e6)
}
