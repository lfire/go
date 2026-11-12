package main

import (
	"sync"
	"testing"
)

type Histogram struct{ count uint64 }

func (h *Histogram) Observe() {
	h.count++
}

func TestHistogramRace(t *testing.T) {
	h := &Histogram{}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.Observe()
		}()
	}
	wg.Wait()
}
