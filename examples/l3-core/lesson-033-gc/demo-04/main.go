package main

import (
	"fmt"
	"runtime/debug"
	"runtime/metrics"
)

var sink [][]byte

func main() {
	for _, pct := range []int{50, 100, 200, -1} {
		debug.SetGCPercent(pct)
		live := make([][]byte, 40)
		for i := range live {
			live[i] = make([]byte, 1<<20)
		}
		s := make([]metrics.Sample, 1)
		s[0].Name = "/gc/heap/goal:bytes"
		metrics.Read(s)
		g := s[0].Value.Uint64()
		if pct < 0 {
			fmt.Printf("GOGC= off → goal=%d（哨兵值，约 7 EiB）\n", g)
		} else {
			fmt.Printf("GOGC=%4d → goal=%d MiB\n", pct, g>>20)
		}
		sink = live
	}
}
