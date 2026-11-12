package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

var hold [][]byte

func main() {
	debug.SetGCPercent(20)
	for i := 0; i < 40; i++ {
		hold = append(hold, make([]byte, 1<<20))
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Println("NumGC:", m.NumGC, "NextGC MiB:", m.NextGC>>20)
}
