package main

import (
	"os"
	"sync/atomic"
	"testing"
)

type Event struct {
	Payload [1024]byte
}

var sinkByte atomic.Int64

// badHandoff 传指针，发送方在交接后继续改写对象，构成数据竞争
func badHandoff() int {
	ch := make(chan *Event, 1)
	go func() {
		e := &Event{}
		ch <- e
		e.Payload[0] = 9 // 交出去之后还改
	}()
	e := <-ch
	return int(e.Payload[0])
}

// goodHandoff 传值，交接是一次完整拷贝，之后各不相干
func goodHandoff() int {
	ch := make(chan Event, 1)
	go func() {
		var e Event
		e.Payload[0] = 7
		ch <- e
		e.Payload[0] = 9 // 改的是本地副本
	}()
	e := <-ch
	return int(e.Payload[0])
}

func TestHandoffValue(t *testing.T) {
	if got := goodHandoff(); got != 7 {
		t.Fatalf("期望 7，实际 %d", got)
	}
}

// 反面实验：设 SHOW_RACE=1 并配合 -race，暴露传指针后改写的 DATA RACE
func TestPointerHandoffRace(t *testing.T) {
	if os.Getenv("SHOW_RACE") == "" {
		t.Skip("反面实验：设 SHOW_RACE=1 并加 -race 运行")
	}
	_ = badHandoff()
}

func BenchmarkByValue(b *testing.B) {
	ch := make(chan Event, 1)
	for i := 0; i < b.N; i++ {
		ch <- Event{}
		e := <-ch
		sinkByte.Store(int64(e.Payload[0]))
	}
}

func BenchmarkByPointer(b *testing.B) {
	ch := make(chan *Event, 1)
	for i := 0; i < b.N; i++ {
		ch <- &Event{}
		e := <-ch
		sinkByte.Store(int64(e.Payload[0]))
	}
}
