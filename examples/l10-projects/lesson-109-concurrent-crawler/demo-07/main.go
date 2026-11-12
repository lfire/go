package main

import (
	"fmt"
	"time"
)

type Task struct {
	URL     string
	Attempt int
}

type Lease struct {
	ID     int64
	Task   Task
	Expiry time.Time
	Epoch  int64
}

// Broker 是 V3 队列核心：待处理、在途租约、完成去重表、死信。
type Broker struct {
	pending  []Task
	inflight map[int64]Lease
	done     map[string]int64 // url -> 完成时 epoch
	dead     []Task
	nextID   int64
	epoch    int64
	ttl      time.Duration
	maxTry   int
}

func NewBroker(ttl time.Duration, maxTry int) *Broker {
	return &Broker{
		inflight: map[int64]Lease{},
		done:     map[string]int64{},
		ttl:      ttl,
		maxTry:   maxTry,
	}
}

// Push 入队前先看去重表，已完成的任务不再入队。
func (b *Broker) Push(t Task) {
	if _, ok := b.done[t.URL]; ok {
		return
	}
	b.pending = append(b.pending, t)
}

// Lease 领取任务：先把超时未确认的租约重投，再取队首。
func (b *Broker) Lease() (Lease, bool) {
	now := time.Now()
	for id, l := range b.inflight {
		if now.After(l.Expiry) {
			delete(b.inflight, id)
			if l.Task.Attempt >= b.maxTry {
				b.dead = append(b.dead, l.Task) // 进死信
				continue
			}
			l.Task.Attempt++
			b.pending = append(b.pending, l.Task)
			fmt.Println("  租约超时重投:", l.Task.URL, "第", l.Task.Attempt, "次")
		}
	}
	if len(b.pending) == 0 {
		return Lease{}, false
	}
	t := b.pending[0]
	b.pending = b.pending[1:]
	b.nextID++
	l := Lease{ID: b.nextID, Task: t, Expiry: now.Add(b.ttl), Epoch: b.epoch}
	b.inflight[l.ID] = l
	return l, true
}

// Ack 确认：租约必须仍在 inflight，且写 done 幂等判定。
func (b *Broker) Ack(l Lease) bool {
	if _, ok := b.inflight[l.ID]; !ok {
		return false // 迟到确认作废
	}
	delete(b.inflight, l.ID)
	if prev, ok := b.done[l.Task.URL]; ok && prev >= l.Epoch {
		return false // 重复投递，幂等键挡住
	}
	b.done[l.Task.URL] = l.Epoch
	return true
}

func (b *Broker) Drain() {
	for {
		l, ok := b.Lease()
		if !ok {
			break
		}
		b.Ack(l)
	}
}

func main() {
	b := NewBroker(20*time.Millisecond, 3)
	for _, u := range []string{"/a", "/b", "/a"} {
		b.Push(Task{URL: u})
	}
	fmt.Println("入队后待处理:", len(b.pending))
	l1, _ := b.Lease()
	fmt.Println("worker1 领取:", l1.Task.URL, "（模拟崩溃，不 Ack）")
	time.Sleep(25 * time.Millisecond)
	b.Drain()
	fmt.Println("已完成:", len(b.done), "死信:", len(b.dead))
}
