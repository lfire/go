package main

import (
	"fmt"
	"sync"
	"time"
)

type Message struct {
	ID      int
	Body    string
	Retries int
}

type Broker struct {
	mu       sync.Mutex
	pending  map[int]Message // 已投递但未 ACK 的消息
	dlq      []Message       // 死信队列
	ch       chan Message
	nextID   int
	maxRetry int
}

func NewBroker(maxRetry int) *Broker {
	return &Broker{
		pending:  make(map[int]Message),
		ch:       make(chan Message, 16),
		maxRetry: maxRetry,
	}
}

func (b *Broker) Publish(body string) {
	b.mu.Lock()
	b.nextID++
	m := Message{ID: b.nextID, Body: body}
	b.pending[m.ID] = m
	b.mu.Unlock()
	b.ch <- m
}

// Ack 只有消费成功后才调用，Broker 此时才把消息从待办里移除。
func (b *Broker) Ack(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, id)
}

// Nack 失败重投：未超过次数就重新入队，否则进死信队列。
func (b *Broker) Nack(m Message) {
	m.Retries++
	b.mu.Lock()
	defer b.mu.Unlock()
	if m.Retries > b.maxRetry {
		delete(b.pending, m.ID)
		b.dlq = append(b.dlq, m)
		fmt.Println("进入死信队列:", m.Body)
		return
	}
	go func() { b.ch <- m }() // 异步重投，避免持锁阻塞
}

func main() {
	b := NewBroker(2)
	b.Publish("order-1")
	b.Publish("order-2")

loop:
	for i := 0; i < 10; i++ {
		select {
		case m := <-b.ch:
			if m.Body == "order-2" && m.Retries < 3 {
				fmt.Printf("处理 %s 第 %d 次失败，重投\n", m.Body, m.Retries+1)
				b.Nack(m)
			} else {
				fmt.Println("处理成功:", m.Body)
				b.Ack(m.ID)
			}
		case <-time.After(200 * time.Millisecond):
			break loop // 队列已空
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	fmt.Println("死信条数:", len(b.dlq))
	fmt.Println("未确认条数:", len(b.pending))
}
