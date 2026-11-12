package main

import (
	"fmt"
	"sync/atomic"
)

type SafeClient struct{ seq atomic.Uint64 }

// Next 用原子自增分配 ID，多 goroutine 并发也保证唯一且递增。
func (c *SafeClient) Next() uint64 { return c.seq.Add(1) }

func main() {
	c := &SafeClient{}
	fmt.Println(c.Next(), c.Next(), c.Next())
}
