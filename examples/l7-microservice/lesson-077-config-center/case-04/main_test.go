package main

import (
	"sync"
	"testing"
)

type Config struct {
	Timeout int
	Retry   int
}

var cfg Config

func mutate() {
	cfg.Timeout = 800
	cfg.Retry = 3
}

func TestTornConfig(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			mutate()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = cfg.Timeout + cfg.Retry
		}
	}()
	wg.Wait()
}
