package main

import (
	"fmt"
	"sort"
)

type Event struct {
	TS     int64
	Level  string
	Status int
	Bytes  int
}

type Window struct {
	Start  int64
	Count  int64
	Bytes  int64
	Errors int64
}

func (w *Window) add(e Event) {
	w.Count++
	w.Bytes += int64(e.Bytes)
	if e.Status >= 500 {
		w.Errors++
	}
}

// Watermark 用「已见最大事件时间减容忍」表示水位线：
// 水位线之下的窗口被认为不会再有正常到达的数据，可以安全关闭。
type Watermark struct {
	maxSeen int64
	allowed int64
}

func (wm *Watermark) Advance(ts int64) {
	if ts > wm.maxSeen {
		wm.maxSeen = ts
	}
}

func (wm *Watermark) Value() int64 { return wm.maxSeen - wm.allowed }

// WindowTable 按窗口起点聚合，水位线推进时关闭已完成的窗口。
type WindowTable struct {
	size   int64
	wm     Watermark
	open   map[int64]*Window
	closed []Window
	late   int64
}

func NewWindowTable(size, allowed int64) *WindowTable {
	return &WindowTable{
		size: size,
		wm:   Watermark{allowed: allowed},
		open: make(map[int64]*Window),
	}
}

// Add 按事件时间归窗；窗口已结算才到达的事件计入迟到，不再改写历史结果。
func (t *WindowTable) Add(e Event) {
	t.wm.Advance(e.TS)
	start := e.TS - e.TS%t.size
	w, ok := t.open[start]
	if !ok {
		if start+t.size <= t.wm.Value() {
			t.late++
			return
		}
		w = &Window{Start: start}
		t.open[start] = w
	}
	w.add(e)
	t.drain()
}

// drain 关闭所有末端已不高于水位线的窗口，产出稳定结果。
func (t *WindowTable) drain() {
	for k, w := range t.open {
		if k+t.size <= t.wm.Value() {
			t.closed = append(t.closed, *w)
			delete(t.open, k)
		}
	}
}

// Flush 输入结束时强制结算剩余窗口，否则末尾不足一窗的数据永远不产出。
func (t *WindowTable) Flush() []Window {
	for _, w := range t.open {
		t.closed = append(t.closed, *w)
	}
	t.open = make(map[int64]*Window)
	sort.Slice(t.closed, func(i, j int) bool {
		return t.closed[i].Start < t.closed[j].Start
	})
	return t.closed
}

func main() {
	t := NewWindowTable(60, 5)
	for _, ev := range []Event{
		{TS: 10, Status: 200, Bytes: 100},
		{TS: 50, Status: 500, Bytes: 200},
		{TS: 65, Status: 200, Bytes: 50},
		{TS: 8, Status: 404, Bytes: 10},
	} {
		t.Add(ev)
	}
	for _, w := range t.Flush() {
		fmt.Printf("窗口起点=%d 条数=%d 字节=%d 错误=%d\n", w.Start, w.Count, w.Bytes, w.Errors)
	}
	fmt.Println("迟到丢弃:", t.late)
}
