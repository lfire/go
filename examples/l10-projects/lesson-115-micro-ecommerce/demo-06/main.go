// 文件：outbox.go —— 本地消息表：业务与消息同事务，崩溃后重放不丢
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrSendFailed = errors.New("outbox: send failed")

type Message struct {
	ID      int64
	Topic   string
	Payload string
	Sent    bool
	Tries   int
}

// Outbox 用锁模拟一张表：Append 与业务写入在同一事务里完成。
// 生产里是 INSERT INTO orders 与 INSERT INTO outbox 包在一个本地事务。
type Outbox struct {
	mu     sync.Mutex
	rows   []Message
	nextID int64
	maxTry int
}

func NewOutbox(maxTry int) *Outbox {
	return &Outbox{maxTry: maxTry}
}

// Append 在业务事务内调用；返回消息 ID 便于对账。
func (o *Outbox) Append(topic, payload string) int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.nextID++
	o.rows = append(o.rows, Message{ID: o.nextID, Topic: topic, Payload: payload})
	return o.nextID
}

// Pending 拉取未发送且未超预算的消息，模拟 SELECT ... WHERE sent=0 LIMIT n。
func (o *Outbox) Pending(limit int) []Message {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]Message, 0, limit)
	for i := range o.rows {
		if !o.rows[i].Sent && o.rows[i].Tries < o.maxTry {
			out = append(out, o.rows[i])
			if len(out) == limit {
				break
			}
		}
	}
	return out
}

// Mark 更新发送结果；失败只累加重试次数，消息保留等待下次扫描。
func (o *Outbox) Mark(id int64, ok bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := range o.rows {
		if o.rows[i].ID == id {
			if ok {
				o.rows[i].Sent = true
			} else {
				o.rows[i].Tries++
			}
			return
		}
	}
}

// Dispatcher 是后台投递 worker。send 为幂等的 MQ 生产者（按消息 ID 去重）。
type Dispatcher struct {
	outbox *Outbox
	send   func(Message) error
}

func (d *Dispatcher) RunOnce(batch int) (sent, failed int) {
	for _, m := range d.outbox.Pending(batch) {
		if err := d.send(m); err != nil {
			d.outbox.Mark(m.ID, false)
			failed++
			continue
		}
		d.outbox.Mark(m.ID, true)
		sent++
	}
	return sent, failed
}

func main() {
	ob := NewOutbox(3)
	ob.Append("order.paid", "O-1001")
	ob.Append("order.paid", "O-1002")
	attempts := 0
	d := &Dispatcher{outbox: ob, send: func(m Message) error {
		attempts++
		// 模拟第一次投递整体失败，第二轮才成功。
		if attempts <= 2 {
			return ErrSendFailed
		}
		return nil
	}}
	for round := 1; round <= 3; round++ {
		s, f := d.RunOnce(10)
		fmt.Printf("round=%d sent=%d failed=%d\n", round, s, f)
		time.Sleep(time.Millisecond)
	}
}
