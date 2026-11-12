package main

import (
	"fmt"
	"runtime/debug"
	"runtime/metrics"
)

func main() {
	debug.SetGCPercent(50)
	var sink [][]byte
	for i := 0; i < 40; i++ {
		sink = append(sink, make([]byte, 1<<20))
	}
	names := []string{
		"/gc/heap/goal:bytes",
		"/gc/heap/objects:objects",
		"/memory/classes/heap/objects:bytes",
		"/memory/classes/total:bytes",
	}
	samples := make([]metrics.Sample, len(names))
	for i, n := range names {
		samples[i].Name = n
	}
	metrics.Read(samples)
	for _, s := range samples {
		fmt.Printf("%s = %d\n", s.Name, s.Value.Uint64())
	}
	_ = sink
}
