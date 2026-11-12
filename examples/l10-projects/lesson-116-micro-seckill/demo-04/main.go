// 文件：queue.go —— 有界队列把洪峰摊平成匀速写库，失败回补不丢单
package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// OrderSink 抽象最终落库；Save 必须按排队号或用户 ID 幂等。
type OrderSink interface {
	Save(ctx context.Context, userID string) error
}

type countingSink struct{ saved int64 }

func (c *countingSink) Save(ctx context.Context, userID string) error {
	atomic.AddInt64(&c.saved, 1)
	return nil
}

// Queue 的 worker 匀速消费，失败按预算重试，仍失败交给对账兜底。
type Queue struct {
	ch     chan string
	sink   OrderSink
	wg     sync.WaitGroup
	maxTry int
	onFail func(userID string)
	failed int64
}

func NewQueue(size, workers int, sink OrderSink, onFail func(string)) *Queue {
	q := &Queue{ch: make(chan string, size), sink: sink, maxTry: 3, onFail: onFail}
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}
	return q
}

func (q *Queue) worker() {
	defer q.wg.Done()
	for userID := range q.ch {
		ok := false
		for attempt := 1; attempt <= q.maxTry; attempt++ {
			if q.sink.Save(context.Background(), userID) == nil {
				ok = true
				break
			}
			time.Sleep(time.Duration(attempt) * time.Millisecond)
		}
		if !ok {
			atomic.AddInt64(&q.failed, 1)
			q.onFail(userID) // 回补预扣，避免抢到却没有订单
		}
	}
}

// Enqueue 非阻塞入队：队列满说明下游饱和，直接返回让上层回补并快速失败。
func (q *Queue) Enqueue(userID string) bool {
	select {
	case q.ch <- userID:
		return true
	default:
		return false
	}
}

func (q *Queue) Close() {
	close(q.ch)
	q.wg.Wait()
}

func main() {
	sink := &countingSink{}
	refunded := 0
	var mu sync.Mutex
	q := NewQueue(200, 4, sink, func(string) {
		mu.Lock()
		refunded++
		mu.Unlock()
	})
	for i := 0; i < 100; i++ {
		q.Enqueue(fmt.Sprintf("u-%d", i))
	}
	q.Close()
	fmt.Printf("saved=%d failed=%d refunded=%d\n",
		atomic.LoadInt64(&sink.saved), atomic.LoadInt64(&q.failed), refunded)
}
