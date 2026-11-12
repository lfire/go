package main

import (
	"sync"
	"testing"
)

type Bucket struct{ tokens float64 }

func (b *Bucket) Allow() bool {
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func TestBucketRace(t *testing.T) {
	b := &Bucket{tokens: 1000}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = b.Allow()
		}()
	}
	wg.Wait()
}
