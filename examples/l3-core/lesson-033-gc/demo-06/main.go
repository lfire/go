package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

var live [][]byte

func nextGC() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.NextGC
}

func main() {
	live = make([][]byte, 20)
	for i := range live {
		live[i] = make([]byte, 1<<20)
	}
	fmt.Println("20 MiB 存活, GOGC=100 NextGC =", nextGC()>>20, "MiB")
	debug.SetMemoryLimit(40 << 20)
	fmt.Println("Limit=40MiB      NextGC =", nextGC()>>20, "MiB")
	debug.SetMemoryLimit(24 << 20)
	fmt.Println("Limit=24MiB      NextGC =", nextGC()>>20, "MiB")
	_ = live
}
