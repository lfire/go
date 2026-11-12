package main

import (
	"sync"
	"testing"
)

type Client struct{ seq uint64 }

func (c *Client) next() uint64 {
	c.seq++ // 与 4.1 相同的写法：读改写不是原子的
	return c.seq
}

func TestSeqUnderRace(t *testing.T) {
	c := &Client{}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.next()
		}()
	}
	wg.Wait()
}
